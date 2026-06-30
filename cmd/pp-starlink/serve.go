package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"pp-starlink/internal/db"
	"pp-starlink/internal/orbit"
	"pp-starlink/internal/starlink"
)

//go:embed web/dist
var webDist embed.FS

func cmdServe(cfg Config) {
	port := flagVal("--port")
	if port == "" {
		port = "7070"
	}
	if _, err := strconv.Atoi(port); err != nil {
		fmt.Fprintf(os.Stderr, "invalid --port: %s\n", port)
		os.Exit(1)
	}

	database := mustDB(cfg.DBPath)
	defer database.Close()

	mux := http.NewServeMux()

	// ── API routes ─────────────────────────────────────────────────────────
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sc, err := starlink.Dial(cfg.DishAddr)
		if err != nil {
			jsonError(w, fmt.Sprintf("dial dish: %v", err), http.StatusServiceUnavailable)
			return
		}
		defer sc.Close()

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		var (
			mu                                sync.Mutex
			status                            starlink.Status
			info                              starlink.DeviceInfo
			history                           starlink.History
			location                          starlink.Location
			statErr, infoErr, histErr, locErr error
			wg                                sync.WaitGroup
		)
		wg.Add(4)
		go func() { defer wg.Done(); mu.Lock(); status, statErr = sc.GetStatus(ctx); mu.Unlock() }()
		go func() { defer wg.Done(); mu.Lock(); info, infoErr = sc.GetDeviceInfo(ctx); mu.Unlock() }()
		go func() { defer wg.Done(); mu.Lock(); history, histErr = sc.GetHistory(ctx); mu.Unlock() }()
		go func() { defer wg.Done(); mu.Lock(); location, locErr = sc.GetLocation(ctx); mu.Unlock() }()
		wg.Wait()

		if statErr != nil {
			jsonError(w, fmt.Sprintf("GetStatus: %v", statErr), http.StatusServiceUnavailable)
			return
		}
		if infoErr != nil {
			log.Printf("serve /api/status GetDeviceInfo: %v (continuing)", infoErr)
		}
		if histErr != nil {
			log.Printf("serve /api/status GetHistory: %v (continuing)", histErr)
		}
		if locErr != nil {
			log.Printf("serve /api/status GetLocation: %v (continuing)", locErr)
		}

		// Build the same statusJSON shape as printStatusJSON uses.
		type outageJSON struct {
			Cause            string  `json:"cause"`
			StartTimestampNs int64   `json:"start_timestamp_ns"`
			DurationMs       float64 `json:"duration_ms"`
			DidSwitch        bool    `json:"did_switch"`
		}
		var outages []outageJSON
		for _, o := range history.Outages {
			if o.StartTimestampNs == 0 {
				continue
			}
			outages = append(outages, outageJSON{
				Cause:            o.Cause,
				StartTimestampNs: o.StartTimestampNs,
				DurationMs:       float64(o.DurationNs) / 1e6,
				DidSwitch:        o.DidSwitch,
			})
		}
		if outages == nil {
			outages = []outageJSON{}
		}

		type respBody struct {
			DishID                 string  `json:"dish_id"`
			HardwareVersion        string  `json:"hardware_version"`
			SoftwareVersion        string  `json:"software_version"`
			Bootcount              int32   `json:"bootcount"`
			UptimeS                uint64  `json:"uptime_s"`
			BoresightAzimuthDeg    float32 `json:"boresight_azimuth_deg"`
			BoresightElevationDeg  float32 `json:"boresight_elevation_deg"`
			TiltAngleDeg           float32 `json:"tilt_angle_deg"`
			AttitudeUncertaintyDeg float32 `json:"attitude_uncertainty_deg"`
			IsSnrAboveNoiseFloor   bool    `json:"is_snr_above_noise_floor"`
			IsSnrPersistentlyLow   bool    `json:"is_snr_persistently_low"`
			MobilityClass          string  `json:"mobility_class,omitempty"`
			IsMovingFastPersisted  bool    `json:"is_moving_fast_persisted"`
			GpsValid               bool    `json:"gps_valid"`
			GpsSats                int32   `json:"gps_sats"`
			Quaternion             *struct {
				W float32 `json:"w"`
				X float32 `json:"x"`
				Y float32 `json:"y"`
				Z float32 `json:"z"`
			} `json:"ned2dish_quaternion,omitempty"`
			Location *struct {
				Lat       float64 `json:"lat"`
				Lon       float64 `json:"lon"`
				AltitudeM float64 `json:"altitude_m"`
				Valid     bool    `json:"valid"`
				Timestamp string  `json:"timestamp,omitempty"`
			} `json:"location,omitempty"`
			POPLatencyMs                float32      `json:"pop_latency_ms"`
			POPDropRate                 float32      `json:"pop_drop_rate"`
			DownlinkBps                 float32      `json:"downlink_bps"`
			UplinkBps                   float32      `json:"uplink_bps"`
			EthSpeedMbps                int32        `json:"eth_speed_mbps"`
			IsCellDisabled              bool         `json:"is_cell_disabled"`
			DLBandwidthRestrictedReason string       `json:"dl_bandwidth_restricted_reason"`
			ULBandwidthRestrictedReason string       `json:"ul_bandwidth_restricted_reason"`
			OutageCause                 string       `json:"outage_cause"`
			Alerts                      []string     `json:"alerts"`
			RecentOutages               []outageJSON `json:"recent_outages_15min"`
		}
		body := respBody{
			DishID:                      info.ID,
			HardwareVersion:             info.HardwareVersion,
			SoftwareVersion:             info.SoftwareVersion,
			Bootcount:                   info.Bootcount,
			UptimeS:                     status.UptimeS,
			BoresightAzimuthDeg:         status.BoresightAzimuthDeg,
			BoresightElevationDeg:       status.BoresightElevationDeg,
			TiltAngleDeg:                status.TiltAngleDeg,
			AttitudeUncertaintyDeg:      status.AttitudeUncertaintyDeg,
			IsSnrAboveNoiseFloor:        status.IsSnrAboveNoiseFloor,
			IsSnrPersistentlyLow:        status.IsSnrPersistentlyLow,
			MobilityClass:               status.MobilityClass,
			IsMovingFastPersisted:       status.IsMovingFastPersisted,
			GpsValid:                    status.GpsValid,
			GpsSats:                     status.GpsSats,
			POPLatencyMs:                status.POPLatencyMs,
			POPDropRate:                 status.POPDropRate,
			DownlinkBps:                 status.DownlinkBps,
			UplinkBps:                   status.UplinkBps,
			EthSpeedMbps:                status.EthSpeedMbps,
			IsCellDisabled:              status.IsCellDisabled,
			DLBandwidthRestrictedReason: status.DLBandwidthRestrictedReason,
			ULBandwidthRestrictedReason: status.ULBandwidthRestrictedReason,
			OutageCause:                 status.OutageCause,
			Alerts:                      activeAlerts(status.Alerts),
			RecentOutages:               outages,
		}
		if status.Quaternion != nil {
			body.Quaternion = &struct {
				W float32 `json:"w"`
				X float32 `json:"x"`
				Y float32 `json:"y"`
				Z float32 `json:"z"`
			}{
				W: status.Quaternion.W,
				X: status.Quaternion.X,
				Y: status.Quaternion.Y,
				Z: status.Quaternion.Z,
			}
		}
		if location.Valid {
			body.Location = &struct {
				Lat       float64 `json:"lat"`
				Lon       float64 `json:"lon"`
				AltitudeM float64 `json:"altitude_m"`
				Valid     bool    `json:"valid"`
				Timestamp string  `json:"timestamp,omitempty"`
			}{
				Lat:       location.Lat,
				Lon:       location.Lon,
				AltitudeM: location.AltitudeM,
				Valid:     true,
			}
			if !location.Timestamp.IsZero() {
				body.Location.Timestamp = location.Timestamp.UTC().Format(time.RFC3339Nano)
			}
		}
		jsonOK(w, body)
	})

	mux.HandleFunc("/api/insights", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		events, err := database.QueryInsights(cfg.LossThreshold, cfg.SNRDelta, cfg.NoiseDelta)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type row struct {
			Timestamp                string   `json:"timestamp"`
			PacketLoss               float64  `json:"packet_loss"`
			BeaconSNR                *float64 `json:"beacon_snr,omitempty"`
			NoiseFloor               *float64 `json:"noise_floor,omitempty"`
			BaselineSNR              *float64 `json:"baseline_snr,omitempty"`
			BaselineNoise            *float64 `json:"baseline_noise,omitempty"`
			LowerSignalThanPredicted bool     `json:"lower_signal_than_predicted"`
			IsSnrAboveNoiseFloor     bool     `json:"is_snr_above_noise_floor"`
			Cause                    string   `json:"cause"`
			SatelliteID              *string  `json:"satellite_id,omitempty"`
			Azimuth                  *float64 `json:"azimuth,omitempty"`
			Elevation                *float64 `json:"elevation,omitempty"`
		}
		out := make([]row, len(events))
		for i, e := range events {
			out[i] = row{
				Timestamp:                e.Timestamp.Format(time.RFC3339),
				PacketLoss:               e.PacketLoss,
				BeaconSNR:                e.BeaconSNR,
				NoiseFloor:               e.NoiseFloor,
				BaselineSNR:              e.BaselineSNR,
				BaselineNoise:            e.BaselineNoise,
				LowerSignalThanPredicted: e.LowerSignalThanPredicted,
				IsSnrAboveNoiseFloor:     e.IsSnrAboveNoiseFloor,
				Cause:                    e.Cause,
				SatelliteID:              e.SatelliteID,
				Azimuth:                  e.Azimuth,
				Elevation:                e.Elevation,
			}
		}
		jsonOK(w, out)
	})

	mux.HandleFunc("/api/buckets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		lt := cfg.LossThreshold
		if s := r.URL.Query().Get("loss_threshold"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				lt = v
			}
		}
		buckets, err := database.SpatialBuckets(lt)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type row struct {
			AzBucket  float64 `json:"az_bucket"`
			ElBucket  float64 `json:"el_bucket"`
			AvgLoss   float64 `json:"avg_loss"`
			Incidents int     `json:"incidents"`
		}
		out := make([]row, len(buckets))
		for i, b := range buckets {
			out[i] = row{b.AzBucket, b.ElBucket, b.AvgLoss, b.Incidents}
		}
		jsonOK(w, out)
	})

	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rpt, err := database.QueryReport(db.ReportParams{
			LossThreshold: cfg.LossThreshold, SatLimit: 12,
		})
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type summaryJSON struct {
			TotalSamples   int     `json:"total_samples"`
			FirstSample    string  `json:"first_sample"`
			LastSample     string  `json:"last_sample"`
			AvgLoss        float64 `json:"avg_loss"`
			MaxLoss        float64 `json:"max_loss"`
			AvgObstruction float64 `json:"avg_obstruction"`
			MaxObstruction float64 `json:"max_obstruction"`
			AvgLocalJitter float64 `json:"avg_local_jitter"`
			MaxLocalJitter float64 `json:"max_local_jitter"`
		}
		type dayJSON struct {
			Day            string  `json:"day"`
			Samples        int     `json:"samples"`
			Drops          int     `json:"drops"`
			AvgLoss        float64 `json:"avg_loss"`
			AvgObstruction float64 `json:"avg_obstruction"`
			AvgLocalJitter float64 `json:"avg_local_jitter"`
		}
		type hourJSON struct {
			Hour    int     `json:"hour"`
			Samples int     `json:"samples"`
			Drops   int     `json:"drops"`
			AvgLoss float64 `json:"avg_loss"`
		}
		type bucketJSON struct {
			AzBucket  float64 `json:"az_bucket"`
			ElBucket  float64 `json:"el_bucket"`
			AvgLoss   float64 `json:"avg_loss"`
			Incidents int     `json:"incidents"`
		}
		type respBody struct {
			Summary summaryJSON  `json:"summary"`
			Days    []dayJSON    `json:"days"`
			Hours   []hourJSON   `json:"hours"`
			Buckets []bucketJSON `json:"buckets"`
		}

		s := rpt.Summary
		days := make([]dayJSON, len(rpt.Days))
		for i, d := range rpt.Days {
			days[i] = dayJSON{d.Day, d.Samples, d.Drops, d.AvgLoss, d.AvgObstruction, d.AvgLocalJitter}
		}
		hours := make([]hourJSON, len(rpt.Hours))
		for i, h := range rpt.Hours {
			hours[i] = hourJSON{h.Hour, h.Samples, h.Drops, h.AvgLoss}
		}
		bkts := make([]bucketJSON, len(rpt.Buckets))
		for i, b := range rpt.Buckets {
			bkts[i] = bucketJSON{b.AzBucket, b.ElBucket, b.AvgLoss, b.Incidents}
		}

		fmtT := func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format(time.RFC3339)
		}

		jsonOK(w, respBody{
			Summary: summaryJSON{
				TotalSamples:   s.TotalSamples,
				FirstSample:    fmtT(s.FirstSample),
				LastSample:     fmtT(s.LastSample),
				AvgLoss:        s.AvgLoss,
				MaxLoss:        s.MaxLoss,
				AvgObstruction: s.AvgObstruction,
				MaxObstruction: s.MaxObstruction,
				AvgLocalJitter: s.AvgLocalJitter,
				MaxLocalJitter: s.MaxLocalJitter,
			},
			Days:    days,
			Hours:   hours,
			Buckets: bkts,
		})
	})

	mux.HandleFunc("/api/predict", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		durStr := r.URL.Query().Get("duration")
		if durStr == "" {
			durStr = "60"
		}
		mins, err := strconv.Atoi(durStr)
		if err != nil || mins <= 0 {
			jsonError(w, "invalid duration parameter", http.StatusBadRequest)
			return
		}

		latStr, err := database.GetConfig("latitude")
		if err == sql.ErrNoRows {
			jsonError(w, "observer location not set — run: pp-starlink set-location --lat <float> --lon <float>", http.StatusUnprocessableEntity)
			return
		} else if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lonStr, err := database.GetConfig("longitude")
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lat, _ := strconv.ParseFloat(latStr, 64)
		lon, _ := strconv.ParseFloat(lonStr, 64)

		tles, err := orbit.FetchTLEs(cfg.TLECacheFile)
		if err != nil {
			jsonError(w, fmt.Sprintf("TLE fetch: %v", err), http.StatusServiceUnavailable)
			return
		}
		buckets, err := database.SpatialBuckets(cfg.LossThreshold)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		bad := make([]orbit.BadZone, len(buckets))
		for i, b := range buckets {
			bad[i] = orbit.BadZone{Az: b.AzBucket, El: b.ElBucket}
		}

		windows := orbit.PassesInWindow(tles, lat, lon, bad, time.Duration(mins)*time.Minute)
		type row struct {
			Start     string  `json:"start"`
			End       string  `json:"end"`
			SatID     string  `json:"sat_id"`
			Azimuth   float64 `json:"azimuth"`
			Elevation float64 `json:"elevation"`
		}
		out := make([]row, len(windows))
		for i, wp := range windows {
			out[i] = row{
				Start:     wp.Start.Format(time.RFC3339),
				End:       wp.End.Format(time.RFC3339),
				SatID:     wp.SatID,
				Azimuth:   wp.Azimuth,
				Elevation: wp.Elevation,
			}
		}
		jsonOK(w, out)
	})

	// ── Static SPA ────────────────────────────────────────────────────────
	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))

	// Serve SPA: API hits go to mux; everything else falls through to the
	// React app. Unknown paths return index.html for client-side routing.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve an exact file; if missing, serve index.html.
		f, err := sub.Open(r.URL.Path[1:])
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Fallback — let React Router handle the path.
		idx, err := webDist.ReadFile("web/dist/index.html")
		if err != nil {
			http.Error(w, "index.html not found — run `npm run build` in web/", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(idx)
	})

	addr := ":" + port
	log.Printf("Ground Control serving on http://0.0.0.0%s", addr)
	if err := http.ListenAndServe(addr, corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}

// corsMiddleware adds permissive CORS headers so the Vite dev server proxy
// (localhost:5173 → localhost:7070) works without extra config.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
