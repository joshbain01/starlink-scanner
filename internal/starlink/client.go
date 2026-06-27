package starlink

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "pp-starlink/proto/device"
)

type Client struct {
	conn *grpc.ClientConn
	svc  pb.DeviceClient
}

func Dial(addr string) (*Client, error) {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, svc: pb.NewDeviceClient(conn)}, nil
}

func (c *Client) Close() { c.conn.Close() }

type Alerts struct {
	MotorsStuck                 bool
	ThermalThrottle             bool
	ThermalShutdown             bool
	MastNotNearVertical         bool
	UnexpectedLocation          bool
	SlowEthernet                bool
	SlowEthernet100             bool
	Roaming                     bool
	InstallPending              bool
	IsHeating                   bool
	PowerSupplyThermalThrottle  bool
	IsPowerSaveIdle             bool
	DbfTelemStale               bool
	LowMotorCurrent             bool
	LowerSignalThanPredicted    bool
	ObstructionMapReset         bool
	DishWaterDetected           bool
	RouterWaterDetected         bool
	UpsuRouterPortSlow          bool
	NoEthernetLink              bool
}

type Status struct {
	UptimeS              uint64
	ObstructionFraction  float32
	CurrentlyObstructed  bool
	IsSnrAboveNoiseFloor bool
	IsSnrPersistentlyLow bool
	POPLatencyMs         float32
	POPDropRate          float32
	DownlinkBps          float32
	UplinkBps            float32
	OutageCause          string
	EthSpeedMbps         int32
	IsCellDisabled       bool

	// Physical dish pointing (from dish firmware)
	BoresightAzimuthDeg          float32
	BoresightElevationDeg        float32
	TiltAngleDeg                 float32
	AttitudeUncertaintyDeg       float32
	DesiredBoresightAzimuthDeg   float32
	DesiredBoresightElevationDeg float32

	// SpaceX throttle reasons (empty string = no limit)
	DLBandwidthRestrictedReason string
	ULBandwidthRestrictedReason string

	Alerts Alerts
}

type OutageEvent struct {
	Cause            string
	StartTimestampNs int64
	DurationNs       uint64
	DidSwitch        bool
}

type History struct {
	Current          uint64
	PopPingDropRate  []float32
	PopPingLatencyMs []float32
	DownlinkBps      []float32
	UplinkBps        []float32
	PowerIn          []float32
	Outages          []OutageEvent
}

type DeviceInfo struct {
	ID              string
	HardwareVersion string
	SoftwareVersion string
	CountryCode     string
	Bootcount       int32
}

func (c *Client) GetStatus(ctx context.Context) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := c.svc.Handle(ctx, &pb.Request{
		Request: &pb.Request_GetStatus{GetStatus: &pb.GetStatusRequest{}},
	})
	if err != nil {
		return Status{}, err
	}

	d := resp.GetDishGetStatus()
	if d == nil {
		return Status{}, nil
	}

	var alerts Alerts
	if a := d.GetAlerts(); a != nil {
		alerts = Alerts{
			MotorsStuck:                a.MotorsStuck,
			ThermalThrottle:            a.ThermalThrottle,
			ThermalShutdown:            a.ThermalShutdown,
			MastNotNearVertical:        a.MastNotNearVertical,
			UnexpectedLocation:         a.UnexpectedLocation,
			SlowEthernet:               a.SlowEthernetSpeeds,
			SlowEthernet100:            a.SlowEthernetSpeeds_100,
			Roaming:                    a.Roaming,
			InstallPending:             a.InstallPending,
			IsHeating:                  a.IsHeating,
			PowerSupplyThermalThrottle: a.PowerSupplyThermalThrottle,
			IsPowerSaveIdle:            a.IsPowerSaveIdle,
			DbfTelemStale:              a.DbfTelemStale,
			LowMotorCurrent:            a.LowMotorCurrent,
			LowerSignalThanPredicted:   a.LowerSignalThanPredicted,
			ObstructionMapReset:        a.ObstructionMapReset,
			DishWaterDetected:          a.DishWaterDetected,
			RouterWaterDetected:        a.RouterWaterDetected,
			UpsuRouterPortSlow:         a.UpsuRouterPortSlow,
			NoEthernetLink:             a.NoEthernetLink,
		}
	}

	outageCause := ""
	if o := d.GetOutage(); o != nil && o.Cause != 0 {
		outageCause = o.Cause.String()
	}

	dlReason := rateLimitString(d.DlBandwidthRestrictedReason)
	ulReason := rateLimitString(d.UlBandwidthRestrictedReason)

	var tiltDeg, attUncert, desiredAz, desiredEl float32
	if al := d.GetAlignmentStats(); al != nil {
		tiltDeg = al.TiltAngleDeg
		attUncert = al.AttitudeUncertaintyDeg
		desiredAz = al.DesiredBoresightAzimuthDeg
		desiredEl = al.DesiredBoresightElevationDeg
	}

	return Status{
		UptimeS:                      d.GetDeviceState().GetUptimeS(),
		ObstructionFraction:          d.GetObstructionStats().GetFractionObstructed(),
		CurrentlyObstructed:          d.GetObstructionStats().GetCurrentlyObstructed(),
		IsSnrAboveNoiseFloor:         d.IsSnrAboveNoiseFloor,
		IsSnrPersistentlyLow:         d.IsSnrPersistentlyLow,
		POPLatencyMs:                 d.GetPopPingLatencyMs(),
		POPDropRate:                  d.GetPopPingDropRate(),
		DownlinkBps:                  d.GetDownlinkThroughputBps(),
		UplinkBps:                    d.GetUplinkThroughputBps(),
		OutageCause:                  outageCause,
		EthSpeedMbps:                 d.EthSpeedMbps,
		IsCellDisabled:               d.IsCellDisabled,
		BoresightAzimuthDeg:          d.BoresightAzimuthDeg,
		BoresightElevationDeg:        d.BoresightElevationDeg,
		TiltAngleDeg:                 tiltDeg,
		AttitudeUncertaintyDeg:       attUncert,
		DesiredBoresightAzimuthDeg:   desiredAz,
		DesiredBoresightElevationDeg: desiredEl,
		DLBandwidthRestrictedReason:  dlReason,
		ULBandwidthRestrictedReason:  ulReason,
		Alerts:                       alerts,
	}, nil
}

func (c *Client) GetHistory(ctx context.Context) (History, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := c.svc.Handle(ctx, &pb.Request{
		Request: &pb.Request_GetHistory{GetHistory: &pb.GetHistoryRequest{}},
	})
	if err != nil {
		return History{}, err
	}

	h := resp.GetDishGetHistory()
	if h == nil {
		return History{}, nil
	}

	outages := make([]OutageEvent, 0, len(h.Outages))
	for _, o := range h.Outages {
		if o == nil || o.Cause == 0 {
			continue
		}
		outages = append(outages, OutageEvent{
			Cause:            o.Cause.String(),
			StartTimestampNs: o.StartTimestampNs,
			DurationNs:       o.DurationNs,
			DidSwitch:        o.DidSwitch,
		})
	}

	return History{
		Current:          h.Current,
		PopPingDropRate:  h.PopPingDropRate,
		PopPingLatencyMs: h.PopPingLatencyMs,
		DownlinkBps:      h.DownlinkThroughputBps,
		UplinkBps:        h.UplinkThroughputBps,
		PowerIn:          h.PowerIn,
		Outages:          outages,
	}, nil
}

func (c *Client) GetDeviceInfo(ctx context.Context) (DeviceInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := c.svc.Handle(ctx, &pb.Request{
		Request: &pb.Request_GetDeviceInfo{GetDeviceInfo: &pb.GetDeviceInfoRequest{}},
	})
	if err != nil {
		return DeviceInfo{}, err
	}

	info := resp.GetGetDeviceInfo().GetDeviceInfo()
	if info == nil {
		return DeviceInfo{}, nil
	}

	return DeviceInfo{
		ID:              info.Id,
		HardwareVersion: info.HardwareVersion,
		SoftwareVersion: info.SoftwareVersion,
		CountryCode:     info.CountryCode,
		Bootcount:       info.Bootcount,
	}, nil
}

// HistoryWindowStats computes derived stats from the last windowSize seconds
// of the history ring buffer ending at current.
func HistoryWindowStats(h History, windowSize int) (maxLatMs, minLatMs float32, briefOutageCount int, briefOutageDurationS float32) {
	n := len(h.PopPingLatencyMs)
	if n == 0 || windowSize <= 0 {
		return
	}
	if windowSize > n {
		windowSize = n
	}

	cur := int(h.Current) % n
	minLatMs = -1
	for i := 0; i < windowSize; i++ {
		idx := (cur - windowSize + i + n) % n
		lat := h.PopPingLatencyMs[idx]
		if lat > maxLatMs {
			maxLatMs = lat
		}
		if minLatMs < 0 || lat < minLatMs {
			minLatMs = lat
		}

		if idx < len(h.PopPingDropRate) && h.PopPingDropRate[idx] >= 1.0 {
			briefOutageCount++
			briefOutageDurationS++
		}
	}
	if minLatMs < 0 {
		minLatMs = 0
	}
	return
}

func rateLimitString(r pb.RateLimitReason) string {
	switch r {
	case pb.RateLimitReason_RATE_LIMIT_NONE, pb.RateLimitReason_RATE_LIMIT_UNKNOWN:
		return ""
	default:
		return r.String()
	}
}
