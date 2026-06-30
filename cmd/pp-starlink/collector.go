package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"pp-starlink/internal/db"
	"pp-starlink/internal/maintenance"
	"pp-starlink/internal/orbit"
	"pp-starlink/internal/ping"
	"pp-starlink/internal/retry"
	"pp-starlink/internal/starlink"
)

// Collector owns all per-tick state and exposes a single Tick method to the
// daemon loop. Reconnection, TLE refresh, DB maintenance, and sample assembly
// are all inside; the caller holds no collection state of its own.
type Collector struct {
	d             *db.DB
	dishAddr      string
	tleCacheFile  string
	grpcurlPath   string
	targets       [3]string // [gateway, POP, public]; empty slot = skip
	lat, lon      float64
	hasObs        bool
	enableDishGPS bool

	sc        *starlink.Client
	grpcFails int

	maint       *maintenance.Orchestrator
	retryPolicy retry.Policy

	tles      []orbit.TLE
	tleExpiry time.Time

	// POP path tracking for path-change signal.
	popIP          string
	popChangedNext bool
	lastPOPDetect  time.Time

	// Rate-limit maintenance ops to protect flash storage.
	// Zero values mean "never run yet" so the first Tick triggers each.
	lastPrune         time.Time
	lastVacuum        time.Time
	lastSchemaRefresh time.Time

	// Location RPC backoff when dish policy blocks access.
	locationFetchBlocked bool
	locationRetryAfter   time.Time
}

// NewCollector wires up a Collector and dials the dish immediately. A failed
// initial dial is not fatal — it will be retried on the first Tick.
func NewCollector(d *db.DB, cfg Config, targets [3]string, lat, lon float64, hasObs bool) *Collector {
	c := &Collector{
		d:             d,
		dishAddr:      cfg.DishAddr,
		tleCacheFile:  cfg.TLECacheFile,
		grpcurlPath:   cfg.GrpcurlPath,
		targets:       targets,
		popIP:         targets[1],
		lat:           lat,
		lon:           lon,
		hasObs:        hasObs,
		enableDishGPS: cfg.EnableDishGPS,
	}
	c.dial()
	c.retryPolicy = retry.ConsecutiveFailures{N: 3}
	c.maint = maintenance.New(
		&maintenance.Interval{Period: 24 * time.Hour, RunFunc: func() error { return c.d.Prune() }},
		&maintenance.Interval{Period: 7 * 24 * time.Hour, RunFunc: func() error { return c.d.Vacuum() }},
	)
	if hasObs {
		if t, err := orbit.FetchTLEs(c.tleCacheFile); err != nil {
			log.Printf("TLE fetch: %v — orbit disabled until next refresh", err)
			c.hasObs = false
		} else {
			c.tles = t
			c.tleExpiry = time.Now().Add(24 * time.Hour)
			log.Printf("loaded %d Starlink TLEs", len(c.tles))
		}
	}
	return c
}

// Close releases the gRPC connection.
func (c *Collector) Close() {
	if c.sc != nil {
		c.sc.Close()
	}
}

// Tick runs one collection cycle: refreshes TLEs and DB maintenance on their
// respective schedules, reconnects gRPC after 3 consecutive failures, then
// assembles and writes a NetworkSample.
func (c *Collector) Tick(ctx context.Context) {
	now := time.Now()
	c.maybeRefreshTLEs(now)
	c.maybeRefreshPOP(now)
	for _, err := range c.maint.Tick(now) {
		log.Printf("maintenance: %v", err)
	}
	c.maybeRefreshSchema(now)

	if c.sc == nil {
		c.dial()
		if c.sc == nil {
			return // still unreachable; skip this tick
		}
	}
	if c.sample(ctx) {
		c.grpcFails = 0
	} else {
		c.grpcFails++
		if c.retryPolicy.ShouldRedial(c.grpcFails) {
			log.Printf("grpc: %d consecutive failures — redialing", c.grpcFails)
			c.dial()
		}
	}
}

func (c *Collector) maybeRefreshPOP(now time.Time) {
	if now.Sub(c.lastPOPDetect) < 30*time.Minute {
		return
	}
	c.lastPOPDetect = now

	pop, err := detectPOP()
	if err != nil {
		log.Printf("POP refresh failed (%v) — keeping current POP target", err)
		return
	}
	if pop == "" {
		return
	}

	if c.popIP == "" {
		c.popIP = pop
		c.targets[1] = pop
		log.Printf("POP IP initialized: %s", pop)
		return
	}

	if pop != c.popIP {
		log.Printf("POP path changed: %s -> %s", c.popIP, pop)
		c.popIP = pop
		c.targets[1] = pop
		c.popChangedNext = true
	}
}

func (c *Collector) dial() {
	if c.sc != nil {
		c.sc.Close()
		c.sc = nil
	}
	sc, err := starlink.Dial(c.dishAddr)
	if err != nil {
		log.Printf("grpc dial: %v — will retry next tick", err)
		return
	}
	c.sc = sc
	c.grpcFails = 0
	log.Printf("dish connected at %s", c.dishAddr)
}

func (c *Collector) maybeRefreshTLEs(now time.Time) {
	if !c.hasObs || now.Before(c.tleExpiry) {
		return
	}
	t, err := orbit.FetchTLEs(c.tleCacheFile)
	if err != nil {
		log.Printf("TLE refresh: %v — using cached set", err)
		return
	}
	c.tles = t
	c.tleExpiry = now.Add(24 * time.Hour)
	log.Printf("TLE set refreshed (%d satellites)", len(c.tles))
}

func (c *Collector) maybePrune(now time.Time) {
	if now.Sub(c.lastPrune) < 24*time.Hour {
		return
	}
	if err := c.d.Prune(); err != nil {
		log.Printf("prune: %v", err)
	}
	c.lastPrune = now
}

func (c *Collector) maybeVacuum(now time.Time) {
	if now.Sub(c.lastVacuum) < 7*24*time.Hour {
		return
	}
	if err := c.d.Vacuum(); err != nil {
		log.Printf("vacuum: %v", err)
	}
	c.lastVacuum = now
}

// maybeRefreshSchema checks whether the live dish gRPC schema has changed
// since the last check. On drift it logs a warning; it never auto-modifies
// the proto. Runs at most once per week to limit flash wear.
func (c *Collector) maybeRefreshSchema(now time.Time) {
	if now.Sub(c.lastSchemaRefresh) < 7*24*time.Hour {
		return
	}
	c.lastSchemaRefresh = now
	if c.grpcurlPath == "" {
		return
	}

	// Describe key messages and hash the combined output.
	msgs := []string{
		"SpaceX.API.Device.DishGetStatusResponse",
		"SpaceX.API.Device.DishAlerts",
		"SpaceX.API.Device.DishGetHistoryResponse",
	}
	h := sha256.New()
	for _, msg := range msgs {
		out, err := exec.Command(c.grpcurlPath, "-plaintext",
			strings.Split(c.dishAddr, ":")[0]+":9200", "describe", msg).Output()
		if err != nil {
			log.Printf("schema refresh: grpcurl %s: %v", msg, err)
			return
		}
		h.Write(out)
	}
	liveHash := fmt.Sprintf("%x", h.Sum(nil))

	stored, err := c.d.GetConfig("schema_hash")
	if err != nil {
		// First time — store and move on.
		_ = c.d.SetConfig("schema_hash", liveHash)
		return
	}
	if stored != liveHash {
		log.Printf("WARNING: dish gRPC schema has changed — run 'make refresh-schema' and update proto/device.proto")
		_ = c.d.SetConfig("schema_hash", liveHash)
	}
}

// sample runs one ping+gRPC cycle and writes the assembled NetworkSample.
// Returns true if the gRPC call succeeded.
func (c *Collector) sample(ctx context.Context) (grpcOK bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in sample: %v", r)
			grpcOK = false
		}
	}()

	var (
		mu       sync.Mutex
		pings    [3]ping.Result
		history  starlink.History
		location starlink.Location
		locErr   error
		locFetch bool
		wg       sync.WaitGroup
	)

	now := time.Now()

	// Kick off pings.
	for i, t := range c.targets {
		if t == "" {
			continue
		}
		wg.Add(1)
		go func(i int, t string) {
			defer wg.Done()
			r, err := ping.Run(t)
			if err != nil {
				log.Printf("ping %s: %v", t, err)
				return
			}
			mu.Lock()
			pings[i] = r
			mu.Unlock()
		}(i, t)
	}

	// Kick off GetHistory in parallel with GetStatus.
	wg.Add(1)
	go func() {
		defer wg.Done()
		h, err := c.sc.GetHistory(ctx)
		if err != nil {
			log.Printf("grpc history: %v", err)
			return
		}
		mu.Lock()
		history = h
		mu.Unlock()
	}()

	// Best-effort location fetch: back off when dish policy blocks access.
	if c.enableDishGPS && !now.Before(c.locationRetryAfter) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loc, err := c.sc.GetLocation(ctx)
			mu.Lock()
			defer mu.Unlock()
			locFetch = true
			locErr = err
			if err == nil {
				location = loc
			}
		}()
	}

	status, grpcErr := c.sc.GetStatus(ctx)
	wg.Wait()

	if grpcErr != nil {
		log.Printf("grpc: %v", grpcErr)
		return false
	}

	if locFetch {
		if locErr != nil {
			if starlink.IsLocationDisabledByPolicyError(locErr) {
				if !c.locationFetchBlocked {
					log.Printf("grpc location disabled by dish policy; retrying every 30m")
				}
				c.locationFetchBlocked = true
				c.locationRetryAfter = now.Add(30 * time.Minute)
			} else {
				log.Printf("grpc location: %v", locErr)
			}
		} else {
			if c.locationFetchBlocked {
				log.Printf("grpc location re-enabled; resuming per-tick location fetch")
			}
			c.locationFetchBlocked = false
			c.locationRetryAfter = time.Time{}
		}
	}

	// Derived window stats from the last 15 seconds of history.
	const windowSecs = 15
	maxLat, minLat, briefCount, briefDur := starlink.HistoryWindowStats(history, windowSecs)

	// Boresight — use nil pointers when the dish reports zero (not yet locked).
	var bAz, bEl, tilt *float32
	if status.BoresightAzimuthDeg != 0 || status.BoresightElevationDeg != 0 {
		az := status.BoresightAzimuthDeg
		el := status.BoresightElevationDeg
		t := status.TiltAngleDeg
		bAz, bEl, tilt = &az, &el, &t
	}

	var mobilityClass *string
	if status.MobilityClass != "" {
		m := status.MobilityClass
		mobilityClass = &m
	}

	isMovingFast := status.IsMovingFastPersisted
	var isMovingFastPtr *bool
	if mobilityClass != nil {
		isMovingFastPtr = &isMovingFast
	}

	gpsValid := status.GpsValid
	gpsSats := status.GpsSats
	var gpsValidPtr *bool
	var gpsSatsPtr *int32
	if status.GpsSats > 0 || status.GpsValid {
		gpsValidPtr = &gpsValid
		gpsSatsPtr = &gpsSats
	}

	var gpsLatPtr, gpsLonPtr, gpsAltPtr *float64
	if location.Valid {
		lat := location.Lat
		lon := location.Lon
		alt := location.AltitudeM
		gpsLatPtr = &lat
		gpsLonPtr = &lon
		gpsAltPtr = &alt
	}

	var quatW, quatX, quatY, quatZ *float64
	if status.Quaternion != nil {
		w := float64(status.Quaternion.W)
		x := float64(status.Quaternion.X)
		y := float64(status.Quaternion.Y)
		z := float64(status.Quaternion.Z)
		quatW, quatX, quatY, quatZ = &w, &x, &y, &z
	}

	s := db.NetworkSample{
		Timestamp:                   now,
		UptimeS:                     status.UptimeS,
		ObstructionFraction:         status.ObstructionFraction,
		CurrentlyObstructed:         status.CurrentlyObstructed,
		ThermalShutdown:             status.Alerts.ThermalShutdown,
		ThermalThrottle:             status.Alerts.ThermalThrottle,
		SlowEthernet:                status.Alerts.SlowEthernet,
		GatewayJitterMs:             pings[0].JitterMs,
		POPJitterMs:                 pings[1].JitterMs,
		POPIP:                       c.popIP,
		POPPathChanged:              c.popChangedNext,
		POPLatencyMs:                float64(status.POPLatencyMs),
		POPDropRate:                 float64(status.POPDropRate),
		PublicPacketLoss:            pings[2].PacketLoss,
		LowerSignalThanPredicted:    status.Alerts.LowerSignalThanPredicted,
		IsSnrAboveNoiseFloor:        status.IsSnrAboveNoiseFloor,
		IsSnrPersistentlyLow:        status.IsSnrPersistentlyLow,
		DownlinkBps:                 status.DownlinkBps,
		UplinkBps:                   status.UplinkBps,
		OutageCause:                 status.OutageCause,
		BoresightAzimuthDeg:         bAz,
		BoresightElevationDeg:       bEl,
		TiltAngleDeg:                tilt,
		DLBandwidthRestrictedReason: status.DLBandwidthRestrictedReason,
		ULBandwidthRestrictedReason: status.ULBandwidthRestrictedReason,
		EthSpeedMbps:                status.EthSpeedMbps,
		IsHeating:                   status.Alerts.IsHeating,
		PowerSupplyThermalThrottle:  status.Alerts.PowerSupplyThermalThrottle,
		DishWaterDetected:           status.Alerts.DishWaterDetected,
		RouterWaterDetected:         status.Alerts.RouterWaterDetected,
		NoEthernetLink:              status.Alerts.NoEthernetLink,
		Roaming:                     status.Alerts.Roaming,
		MobilityClass:               mobilityClass,
		IsMovingFast:                isMovingFastPtr,
		GpsValid:                    gpsValidPtr,
		GpsSats:                     gpsSatsPtr,
		GpsLat:                      gpsLatPtr,
		GpsLon:                      gpsLonPtr,
		GpsAltitudeM:                gpsAltPtr,
		QuatW:                       quatW,
		QuatX:                       quatX,
		QuatY:                       quatY,
		QuatZ:                       quatZ,
		MaxLatencyMs:                maxLat,
		MinLatencyMs:                minLat,
		BriefOutageCount:            briefCount,
		BriefOutageDurationS:        briefDur,
	}

	if c.hasObs {
		if look, err := orbit.HighestSatellite(c.tles, c.lat, c.lon, now); err == nil {
			satID := look.SatID
			az, el := look.Azimuth, look.Elevation
			s.SatelliteID = &satID
			s.Azimuth = &az
			s.Elevation = &el
		} else {
			log.Printf("orbit: %v", err)
		}
	}

	if err := c.d.WriteNetwork(s); err != nil {
		log.Printf("db write: %v", err)
	}
	c.popChangedNext = false

	// Write structured outage events from history (INSERT OR IGNORE, flash-safe).
	if len(history.Outages) > 0 {
		records := make([]db.OutageRecord, len(history.Outages))
		for i, o := range history.Outages {
			records[i] = db.OutageRecord{
				StartTimestampNs: o.StartTimestampNs,
				DurationNs:       o.DurationNs,
				Cause:            o.Cause,
				DidSwitch:        o.DidSwitch,
			}
		}
		if err := c.d.WriteOutageEvents(records); err != nil {
			log.Printf("outage write: %v", err)
		}
	}

	return true
}
