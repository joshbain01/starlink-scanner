package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"pp-starlink/internal/db"
	"pp-starlink/internal/orbit"
	"pp-starlink/internal/starlink"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help" {
		printHelp()
		if len(os.Args) < 2 {
			os.Exit(1)
		}
		return
	}
	cfg := Load()
	switch os.Args[1] {
	case "init":
		d := mustDB(cfg.DBPath)
		d.Close()
		fmt.Println("schema ready")
	case "daemon":
		cmdDaemon(cfg)
	case "insights":
		cmdInsights(cfg, hasFlag("--compact"))
	case "set-location":
		cmdSetLocation(cfg)
	case "predict-window":
		cmdPredictWindow(cfg)
	case "status":
		cmdStatus(cfg)
	case "report":
		cmdReport(cfg)
	case "obstruction-map":
		cmdObstructionMap(cfg)
	case "serve":
		cmdServe(cfg)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func cmdStatus(cfg Config) {
	sc, err := starlink.Dial(cfg.DishAddr)
	if err != nil {
		log.Fatalf("dial %s: %v", cfg.DishAddr, err)
	}
	defer sc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var (
		mu                        sync.Mutex
		status                    starlink.Status
		info                      starlink.DeviceInfo
		history                   starlink.History
		statErr, infoErr, histErr error
		wg                        sync.WaitGroup
	)
	wg.Add(3)
	go func() { defer wg.Done(); mu.Lock(); status, statErr = sc.GetStatus(ctx); mu.Unlock() }()
	go func() { defer wg.Done(); mu.Lock(); info, infoErr = sc.GetDeviceInfo(ctx); mu.Unlock() }()
	go func() { defer wg.Done(); mu.Lock(); history, histErr = sc.GetHistory(ctx); mu.Unlock() }()
	wg.Wait()

	if statErr != nil {
		log.Fatalf("GetStatus: %v", statErr)
	}
	if infoErr != nil {
		log.Printf("GetDeviceInfo: %v (continuing)", infoErr)
	}
	if histErr != nil {
		log.Printf("GetHistory: %v (continuing)", histErr)
	}

	if hasFlag("--json") {
		printStatusJSON(status, info, history)
		return
	}
	printStatusHuman(status, info, history)
}

func printStatusHuman(status starlink.Status, info starlink.DeviceInfo, history starlink.History) {
	uptimeH := float64(status.UptimeS) / 3600.0

	fmt.Printf("Dish:      %s  %s  fw %s\n", info.ID, info.HardwareVersion, info.SoftwareVersion)
	fmt.Printf("Uptime:    %.1fh  Reboots: %d\n", uptimeH, info.Bootcount)
	fmt.Printf("Pointing:  az=%.1f°  el=%.1f°  tilt=%.1f°  uncertainty=%.1f°\n",
		status.BoresightAzimuthDeg, status.BoresightElevationDeg,
		status.TiltAngleDeg, status.AttitudeUncertaintyDeg)
	fmt.Printf("Signal:    snr_above_noise=%v  persistently_low=%v\n",
		status.IsSnrAboveNoiseFloor, status.IsSnrPersistentlyLow)
	fmt.Printf("Throughput: ↓ %.0f Mbps  ↑ %.0f Mbps  POP latency=%.0fms  drop=%.1f%%\n",
		status.DownlinkBps/1e6, status.UplinkBps/1e6,
		status.POPLatencyMs, status.POPDropRate*100)
	fmt.Printf("Ethernet:  %d Mbps\n", status.EthSpeedMbps)

	dlR, ulR := status.DLBandwidthRestrictedReason, status.ULBandwidthRestrictedReason
	if dlR == "" {
		dlR = "none"
	}
	if ulR == "" {
		ulR = "none"
	}
	fmt.Printf("Throttle:  dl=%s  ul=%s\n", dlR, ulR)

	if status.OutageCause != "" {
		fmt.Printf("Outage:    %s\n", status.OutageCause)
	}
	if status.IsCellDisabled {
		fmt.Printf("Cell:      DISABLED\n")
	}

	alerts := activeAlerts(status.Alerts)
	if len(alerts) == 0 {
		fmt.Printf("Alerts:    none\n")
	} else {
		fmt.Printf("Alerts:    %s\n", strings.Join(alerts, ", "))
	}

	if len(history.Outages) > 0 {
		type summary struct {
			cause   string
			count   int
			totalNs uint64
		}
		byC := map[string]*summary{}
		for _, o := range history.Outages {
			if s, ok := byC[o.Cause]; ok {
				s.count++
				s.totalNs += o.DurationNs
			} else {
				byC[o.Cause] = &summary{cause: o.Cause, count: 1, totalNs: o.DurationNs}
			}
		}
		var parts []string
		for _, s := range byC {
			avgMs := float64(s.totalNs) / float64(s.count) / 1e6
			parts = append(parts, fmt.Sprintf("%d× %s ~%.0fms", s.count, s.cause, avgMs))
		}
		fmt.Printf("Outages (15 min history): %s\n", strings.Join(parts, "  "))
	} else {
		fmt.Printf("Outages (15 min history): none\n")
	}
}

type statusJSON struct {
	DishID                      string       `json:"dish_id"`
	HardwareVersion             string       `json:"hardware_version"`
	SoftwareVersion             string       `json:"software_version"`
	Bootcount                   int32        `json:"bootcount"`
	UptimeS                     uint64       `json:"uptime_s"`
	BoresightAzimuthDeg         float32      `json:"boresight_azimuth_deg"`
	BoresightElevationDeg       float32      `json:"boresight_elevation_deg"`
	TiltAngleDeg                float32      `json:"tilt_angle_deg"`
	AttitudeUncertaintyDeg      float32      `json:"attitude_uncertainty_deg"`
	IsSnrAboveNoiseFloor        bool         `json:"is_snr_above_noise_floor"`
	IsSnrPersistentlyLow        bool         `json:"is_snr_persistently_low"`
	POPLatencyMs                float32      `json:"pop_latency_ms"`
	POPDropRate                 float32      `json:"pop_drop_rate"`
	DownlinkBps                 float32      `json:"downlink_bps"`
	UplinkBps                   float32      `json:"uplink_bps"`
	EthSpeedMbps                int32        `json:"eth_speed_mbps"`
	IsCellDisabled              bool         `json:"is_cell_disabled"`
	DLBandwidthRestrictedReason string       `json:"dl_bandwidth_restricted_reason"`
	ULBandwidthRestrictedReason string       `json:"ul_bandwidth_restricted_reason"`
	OutageCause                 string       `json:"outage_cause,omitempty"`
	Alerts                      []string     `json:"alerts"`
	RecentOutages               []outageJSON `json:"recent_outages_15min"`
}

type outageJSON struct {
	Cause            string  `json:"cause"`
	StartTimestampNs int64   `json:"start_timestamp_ns"`
	DurationMs       float64 `json:"duration_ms"`
	DidSwitch        bool    `json:"did_switch"`
}

func printStatusJSON(status starlink.Status, info starlink.DeviceInfo, history starlink.History) {
	outages := make([]outageJSON, len(history.Outages))
	for i, o := range history.Outages {
		outages[i] = outageJSON{
			Cause:            o.Cause,
			StartTimestampNs: o.StartTimestampNs,
			DurationMs:       float64(o.DurationNs) / 1e6,
			DidSwitch:        o.DidSwitch,
		}
	}
	out := statusJSON{
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
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func activeAlerts(a starlink.Alerts) []string {
	var out []string
	check := func(v bool, name string) {
		if v {
			out = append(out, name)
		}
	}
	check(a.ThermalShutdown, "thermal_shutdown")
	check(a.ThermalThrottle, "thermal_throttle")
	check(a.PowerSupplyThermalThrottle, "power_supply_thermal_throttle")
	check(a.IsHeating, "is_heating")
	check(a.MotorsStuck, "motors_stuck")
	check(a.MastNotNearVertical, "mast_not_near_vertical")
	check(a.SlowEthernet, "slow_ethernet")
	check(a.SlowEthernet100, "slow_ethernet_100mbps")
	check(a.NoEthernetLink, "no_ethernet_link")
	check(a.DishWaterDetected, "dish_water_detected")
	check(a.RouterWaterDetected, "router_water_detected")
	check(a.LowerSignalThanPredicted, "lower_signal_than_predicted")
	check(a.Roaming, "roaming")
	check(a.UnexpectedLocation, "unexpected_location")
	check(a.InstallPending, "install_pending")
	check(a.IsPowerSaveIdle, "power_save_idle")
	check(a.LowMotorCurrent, "low_motor_current")
	check(a.ObstructionMapReset, "obstruction_map_reset")
	check(a.UpsuRouterPortSlow, "upsu_router_port_slow")
	return out
}

func cmdReport(cfg Config) {
	d := mustDB(cfg.DBPath)
	defer d.Close()

	rpt, err := d.QueryReport(db.ReportParams{LossThreshold: cfg.LossThreshold, SatLimit: 12})
	if err != nil {
		log.Fatal(err)
	}
	sum := rpt.Summary
	fmt.Printf("## Summary\n")
	fmt.Printf("Samples: %d  |  %s → %s\n", sum.TotalSamples,
		sum.FirstSample.Format("2006-01-02 15:04"), sum.LastSample.Format("2006-01-02 15:04"))
	fmt.Printf("Packet loss:  avg=%.2f%%  max=%.0f%%\n", sum.AvgLoss*100, sum.MaxLoss*100)
	fmt.Printf("Obstruction:  avg=%.1f%%  max=%.1f%%\n", sum.AvgObstruction*100, sum.MaxObstruction*100)
	fmt.Printf("Local jitter: avg=%.1fms  max=%.1fms\n", sum.AvgLocalJitter, sum.MaxLocalJitter)

	if len(rpt.Days) > 0 {
		fmt.Printf("\n## Drop rate by day\n")
		fmt.Printf("  %-12s %7s %6s %6s %8s %8s\n", "Day", "Samples", "Drops", "Drop%", "AvgLoss", "Obs%")
		for _, s := range rpt.Days {
			pct := 0.0
			if s.Samples > 0 {
				pct = float64(s.Drops) / float64(s.Samples) * 100
			}
			fmt.Printf("  %-12s %7d %6d %5.1f%%  %6.2f%%  %5.1f%%\n",
				s.Day, s.Samples, s.Drops, pct, s.AvgLoss*100, s.AvgObstruction*100)
		}
	}

	if len(rpt.Hours) > 0 {
		fmt.Printf("\n## Drop rate by hour of day (all days combined)\n")
		for _, h := range rpt.Hours {
			pct := 0.0
			if h.Samples > 0 {
				pct = float64(h.Drops) / float64(h.Samples) * 100
			}
			bar := strings.Repeat("#", int(pct/2))
			fmt.Printf("  %02d:00  %4.1f%%  %s\n", h.Hour, pct, bar)
		}
	}

	if len(rpt.Buckets) > 0 {
		fmt.Printf("\n## Drop direction compass\n")
		fmt.Printf("  Line length = distance from horizon (long=low elevation, short=near zenith)\n")
		fmt.Printf("  + <20%%  * 20-50%%  # 50-90%%  X >90%% avg loss  ·=el60° ring  outer·=el30° ring\n\n")
		for _, line := range strings.Split(renderCompass(rpt.Buckets), "\n") {
			if line != "" {
				fmt.Printf("  %s\n", line)
			}
		}
		highElCount := 0
		for _, b := range rpt.Buckets {
			if b.ElBucket >= 65 && b.Incidents >= 2 {
				highElCount++
			}
		}
		if highElCount > len(rpt.Buckets)/2 {
			fmt.Printf("  → Drops cluster near zenith (el>65°) across all directions.\n")
			fmt.Printf("    This is a network/POP pattern, not physical blockage.\n")
			fmt.Printf("    Physical blockage would show long spikes toward the horizon.\n")
		}
	}

	if rpt.Bursts.TotalBursts > 0 {
		fmt.Printf("\n## Outage bursts (consecutive 100%% loss)\n")
		fmt.Printf("  Total: %d bursts, %.0f min total outage time\n",
			rpt.Bursts.TotalBursts, rpt.Bursts.TotalOutageTime.Minutes())
		fmt.Printf("  Shape: avg=%.1fm  p95=%.1fm  max=%.1fm  severe(>=3m)=%d\n",
			rpt.Bursts.Shape.AvgMinutes,
			rpt.Bursts.Shape.P95Minutes,
			rpt.Bursts.Shape.MaxMinutes,
			rpt.Bursts.Shape.SevereBursts)
		fmt.Printf("  Worst %d:\n", len(rpt.Bursts.Worst))
		fmt.Printf("    %-22s %-22s %9s\n", "Start", "End", "Duration")
		for _, b := range rpt.Bursts.Worst {
			fmt.Printf("    %-22s %-22s %8.1fm\n",
				b.Start.Format("2006-01-02 15:04:05"),
				b.End.Format("2006-01-02 15:04:05"),
				b.Duration.Minutes())
		}
	}

	if len(rpt.Reboots) > 0 {
		fmt.Printf("\n## Dish reboots\n")
		for _, r := range rpt.Reboots {
			fmt.Printf("  %s  uptime reset → %ds (%.1fh)\n",
				r.At.Format("2006-01-02 15:04:05"), r.NewUptime, float64(r.NewUptime)/3600)
		}
	}

	if len(rpt.Causes) > 0 {
		fmt.Printf("\n## Drop cause breakdown (dish-reported)\n")
		for _, c := range rpt.Causes {
			fmt.Printf("  %-20s %5d drops  avg_loss=%.0f%%\n", c.Cause, c.Samples, c.AvgLoss*100)
		}
	} else {
		fmt.Printf("\n## Drop cause breakdown\n")
		fmt.Printf("  No dish-reported causes yet — restart the daemon to begin collecting.\n")
	}

	if len(rpt.Satellites) > 0 {
		fmt.Printf("\n## Satellites overhead during drops (top %d of %d affected)\n", len(rpt.Satellites), rpt.TotalSatDrops)
		if rpt.TotalSatDrops > 20 {
			fmt.Printf("  Drops spread across %d different satellites — indicates systemic issue, not bad satellite.\n", rpt.TotalSatDrops)
		}
		fmt.Printf("  %-28s %-20s %5s %8s %8s\n", "Satellite", "Generation", "Inc", "AvgLoss", "MaxLoss")
		for _, s := range rpt.Satellites {
			gen := classifySatellite(s.SatID)
			fmt.Printf("  %-28s %-20s %5d  %6.0f%%  %6.0f%%\n",
				s.SatID, gen, s.Incidents, s.AvgLoss*100, s.MaxLoss*100)
		}
	}

	pp := rpt.POPPath
	if pp.SamplesStable+pp.SamplesAfter > 0 {
		fmt.Printf("\n## POP path changes\n")
		fmt.Printf("  Distinct POPs observed: %d\n", pp.DistinctPOPCount)
		if pp.LastPOP != "" {
			fmt.Printf("  Current POP IP: %s\n", pp.LastPOP)
		}
		fmt.Printf("  Path-change events: %d\n", pp.Changes)
		afterPct := 0.0
		if pp.SamplesAfter > 0 {
			afterPct = float64(pp.DropsAfter) / float64(pp.SamplesAfter) * 100
		}
		stablePct := 0.0
		if pp.SamplesStable > 0 {
			stablePct = float64(pp.DropsStable) / float64(pp.SamplesStable) * 100
		}
		fmt.Printf("  Drop rate on change samples: %.1f%% (%d/%d)\n", afterPct, pp.DropsAfter, pp.SamplesAfter)
		fmt.Printf("  Drop rate on stable samples: %.1f%% (%d/%d)\n", stablePct, pp.DropsStable, pp.SamplesStable)
	}

	ho := rpt.Handoffs
	if ho.HandoffSamples > 0 {
		hoPct := float64(ho.HandoffDrops) / float64(ho.HandoffSamples) * 100
		noPct := float64(ho.NonHandoffDrops) / float64(ho.NonHandoffSamples) * 100
		fmt.Printf("\n## Satellite handoff correlation\n")
		fmt.Printf("  At handoff:     %d samples  %.1f%% drop rate\n", ho.HandoffSamples, hoPct)
		fmt.Printf("  Not at handoff: %d samples  %.1f%% drop rate\n", ho.NonHandoffSamples, noPct)
		if hoPct < noPct*1.2 {
			fmt.Printf("  → Handoffs are NOT the cause (rates within 20%%)\n")
		} else {
			fmt.Printf("  → Handoffs correlate with drops (%.1fx higher rate)\n", hoPct/noPct)
		}
	}
}

// renderCompass draws an ASCII sky compass from az/el drop buckets.
// Az is clockwise from North; line length = (90-el)/90 * radius, so a drop
// at the horizon (el≈0) reaches the outer edge and one near zenith (el≈90)
// is a short spike near the center. Severity: + mild  * mod  # severe  X outage
func renderCompass(buckets []db.SpatialBucket) string {
	const radius = 9
	const size = radius*2 + 1 // 19×19

	grid := make([][]rune, size)
	for i := range grid {
		grid[i] = make([]rune, size)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}
	cx, cy := radius, radius
	grid[cy][cx] = '·'

	// Reference rings: dotted circles at el=60° (inner) and el=30° (outer).
	for _, elRef := range []float64{60, 30} {
		r := (90 - elRef) / 90 * float64(radius)
		for deg := 0; deg < 360; deg += 6 {
			rad := float64(deg) * math.Pi / 180
			x := cx + int(math.Round(r*math.Sin(rad)))
			y := cy - int(math.Round(r*math.Cos(rad)))
			if x >= 0 && x < size && y >= 0 && y < size && grid[y][x] == ' ' {
				grid[y][x] = '·'
			}
		}
	}

	// Plot each drop zone as a ray from center. Draw line first, then endpoint
	// so the severity marker overwrites the line char at the tip.
	plotted := 0
	for _, b := range buckets {
		if plotted >= 24 || b.Incidents < 2 {
			break
		}
		lineLen := (90.0 - b.ElBucket) / 90.0 * float64(radius)
		if lineLen < 0.5 {
			lineLen = 0.5 // zenith-level drops still get a center marker
		}
		azRad := b.AzBucket * math.Pi / 180
		ex := cx + int(math.Round(lineLen*math.Sin(azRad)))
		ey := cy - int(math.Round(lineLen*math.Cos(azRad)))

		compassDrawLine(grid, cx, cy, ex, ey, '·')

		ch := '+'
		switch {
		case b.AvgLoss >= 0.9:
			ch = 'X'
		case b.AvgLoss >= 0.5:
			ch = '#'
		case b.AvgLoss >= 0.2:
			ch = '*'
		}
		if ex >= 0 && ex < size && ey >= 0 && ey < size {
			grid[ey][ex] = ch
		}
		plotted++
	}

	pad := strings.Repeat(" ", 2+cx) // aligns N/S label over center column
	var sb strings.Builder
	sb.WriteString(pad + "N\n")
	for row := 0; row < size; row++ {
		if row == cy {
			sb.WriteString("W ")
		} else {
			sb.WriteString("  ")
		}
		sb.WriteString(string(grid[row]))
		if row == cy {
			sb.WriteString(" E")
		}
		sb.WriteByte('\n')
	}
	sb.WriteString(pad + "S\n")
	return sb.String()
}

func compassDrawLine(grid [][]rune, x0, y0, x1, y1 int, ch rune) {
	rows, cols := len(grid), len(grid[0])
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	for {
		if x0 >= 0 && x0 < cols && y0 >= 0 && y0 < rows && grid[y0][x0] == ' ' {
			grid[y0][x0] = ch
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func classifySatellite(name string) string {
	if strings.Contains(name, "[DTC]") {
		return "Direct to Cell"
	}
	numStr := strings.TrimPrefix(strings.Fields(name)[0], "STARLINK-")
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return "unknown"
	}
	switch {
	case num < 2000:
		return "Gen 1 (v1.0)"
	case num < 4500:
		return "Gen 1.5 (v1.5)"
	case num < 6600:
		return "Gen 2 Mini"
	case num < 30000:
		return "Gen 2 / DTC variant"
	default:
		return "Gen 2 Mini (2023+)"
	}
}

func cmdObstructionMap(cfg Config) {
	d := mustDB(cfg.DBPath)
	defer d.Close()

	zones, err := d.QueryObstructionMap()
	if err != nil {
		log.Fatal(err)
	}
	if len(zones) == 0 {
		fmt.Println("No confirmed obstruction events yet.")
		fmt.Println("The daemon needs to collect data with currently_obstructed=true.")
		fmt.Println("This requires the daemon to have run since the last pp-starlink update.")
		return
	}

	fmt.Println("## Physical obstruction map")
	fmt.Println("(Zones where dish reported currently_obstructed=true — aim camera/eyes here)")
	fmt.Printf("  %-6s %-6s %-12s %8s %8s\n", "Az", "El", "Direction", "Incidents", "AvgLoss")
	for _, z := range zones {
		fmt.Printf("  %5.0f° %5.0f°  %-12s %8d  %7.0f%%\n",
			z.AzBucket, z.ElBucket, azToCompass(z.AzBucket), z.Incidents, z.AvgLoss*100)
	}
}

func azToCompass(az float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	return dirs[int((az+11.25)/22.5)%16]
}

func printHelp() {
	fmt.Print(`pp-starlink — Starlink network & RF diagnostics

USAGE
  pp-starlink <command> [flags]

COMMANDS
  status [--json]
    Live dish snapshot — no database required.
    Calls GetStatus, GetDeviceInfo, and GetHistory in parallel against the dish.
    Shows pointing, signal quality, throughput, throttle reasons, active alerts,
    and a summary of all outage events in the last 15 minutes.
    --json   Emit a single JSON object (machine-readable for AI agents).

  init
    Create the SQLite schema and apply tuning pragmas.
    Safe to re-run. Database path: /data/starlink_telemetry.db

  daemon
    Start the collection loop (runs indefinitely).
    Every 15 s: queries the dish gRPC API, pings gateway/POP/public DNS,
    computes satellite look-angles (if location is set), and writes
    telemetry to the database.

  insights [--compact]
    Analyse packet-loss events from the last 30 days.
    Correlates with RF telemetry and classifies each drop as:
      [RF] Blockage/Handoff · [RF] EMI/Radar · [dish] Signal Alert · Downstream/Congestion
    Prints a spatial obstruction map when orbital data is available.
    --compact   Output as Markdown list items (agent-friendly).
    Env vars: STARLINK_LOSS_THRESHOLD (default 0.05)
              STARLINK_SNR_DELTA      (default 3.0 dB)
              STARLINK_NOISE_DELTA    (default 3.0 dB)

  set-location --lat <float> --lon <float>
    Store observer coordinates used for orbital calculations.
    Must be set before the daemon logs satellite az/el or predict-window works.
    --lat <float>   Observer latitude  (e.g. 47.6062)
    --lon <float>   Observer longitude (e.g. -122.3321)
    --compact       Output as a Markdown list item.

	predict-window --duration <minutes> [--synthetic]
    Forecast satellite passes through historically lossy az/el zones.
    Requires set-location to have been run and sufficient daemon history.
    --duration <minutes>   How far ahead to predict (e.g. 60).
		--synthetic            Use built-in dev/demo risk zones when historical
													telemetry zones are unavailable.

  report
    Statistical analysis of collected telemetry: drop rate by day and hour,
    outage bursts, dish reboots, cause breakdown, handoff correlation.
    Read-only — no writes to the database.

  serve [--port <port>]
    Start the Ground Control web dashboard (default port 7070).
    Exposes /api/* endpoints and serves the React SPA.
    Requires the web/ directory to have been built: cd web && npm run build
    --port <port>   Listen port (default 7070).

  obstruction-map
    Show az/el zones where the dish itself flagged currently_obstructed=true.
    These are confirmed physical blockages — look in those compass directions
    at that elevation to find trees, mounts, or structures to clear.
    Requires daemon data collected after the latest update.
    Read-only — no writes to the database.

  help, --help, -h
    Show this help text.
`)
}

func hasFlag(f string) bool {
	for _, a := range os.Args[2:] {
		if a == f {
			return true
		}
	}
	return false
}

func flagVal(name string) string {
	for i, a := range os.Args[2:] {
		if a == name && i+1 < len(os.Args[2:]) {
			return os.Args[3:][i]
		}
	}
	return ""
}

func mustDB(path string) *db.DB {
	d, err := db.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	return d
}

func cmdDaemon(cfg Config) {
	d := mustDB(cfg.DBPath)
	defer d.Close()

	targets := [3]string{"192.168.100.1", "", "1.1.1.1"}
	if pop, err := detectPOP(); err == nil {
		targets[1] = pop
		log.Printf("POP IP: %s", pop)
	} else {
		log.Printf("POP detection failed (%v) — skipping POP pings", err)
	}

	var lat, lon float64
	var hasObs bool
	if latStr, err := d.GetConfig("latitude"); err == nil {
		if lonStr, err2 := d.GetConfig("longitude"); err2 == nil {
			if la, e1 := strconv.ParseFloat(latStr, 64); e1 == nil {
				if lo, e2 := strconv.ParseFloat(lonStr, 64); e2 == nil {
					lat, lon, hasObs = la, lo, true
					log.Printf("observer location: lat=%.4f lon=%.4f", lat, lon)
				}
			}
		}
	}

	c := NewCollector(d, cfg, targets, lat, lon, hasObs)
	defer c.Close()

	log.Printf("daemon started, interval=%s", cfg.Interval)
	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()
	for range tick.C {
		c.Tick(context.Background())
	}
}

func cmdInsights(cfg Config, compact bool) {
	d := mustDB(cfg.DBPath)
	defer d.Close()

	events, err := d.QueryInsights(cfg.LossThreshold, cfg.SNRDelta, cfg.NoiseDelta)
	if err != nil {
		log.Fatal(err)
	}
	if len(events) == 0 {
		fmt.Println("No drop events in last 30 days.")
		return
	}

	for _, e := range events {
		t := e.Timestamp.Format(time.RFC3339)
		sat := fmtSat(e.SatelliteID, e.Azimuth, e.Elevation)
		if compact {
			fmt.Printf("- %s loss=%.0f%%%s %s (conf=%.2f, %s)\n", t, e.PacketLoss*100, sat, e.Cause, e.Confidence, e.ConfidenceWhy)
		} else {
			fmt.Printf("[%s] loss=%.0f%%%s\n  snr=%s (baseline %s)  noise=%s (baseline %s)\n  %s\n  confidence=%.2f (%s)\n\n",
				t, e.PacketLoss*100, sat,
				fmtDB(e.BeaconSNR), fmtDB(e.BaselineSNR),
				fmtDB(e.NoiseFloor), fmtDB(e.BaselineNoise),
				e.Cause, e.Confidence, e.ConfidenceWhy)
		}
	}

	// Spatial obstruction map (only when orbital data has been collected).
	buckets, err := d.SpatialBuckets(cfg.LossThreshold)
	if err != nil {
		log.Printf("spatial buckets: %v", err)
		return
	}
	if len(buckets) == 0 {
		return
	}
	fmt.Println("\n## Obstruction map (az/el buckets with packet loss)")
	fmt.Println("| Az (°) | El (°) | Avg loss | Incidents |")
	fmt.Println("|--------|--------|----------|-----------|")
	for _, b := range buckets {
		fmt.Printf("| %6.0f | %6.0f | %7.1f%% | %9d |\n",
			b.AzBucket, b.ElBucket, b.AvgLoss*100, b.Incidents)
	}
}

func cmdSetLocation(cfg Config) {
	lat, lon := flagVal("--lat"), flagVal("--lon")
	if lat == "" || lon == "" {
		fmt.Fprintln(os.Stderr, "usage: pp-starlink set-location --lat <float> --lon <float>")
		os.Exit(1)
	}
	if _, err := strconv.ParseFloat(lat, 64); err != nil {
		fmt.Fprintf(os.Stderr, "invalid --lat: %s\n", lat)
		os.Exit(1)
	}
	if _, err := strconv.ParseFloat(lon, 64); err != nil {
		fmt.Fprintf(os.Stderr, "invalid --lon: %s\n", lon)
		os.Exit(1)
	}
	d := mustDB(cfg.DBPath)
	defer d.Close()
	if err := d.SetConfig("latitude", lat); err != nil {
		log.Fatal(err)
	}
	if err := d.SetConfig("longitude", lon); err != nil {
		log.Fatal(err)
	}
	if hasFlag("--compact") {
		fmt.Printf("- location: lat=%s lon=%s\n", lat, lon)
	} else {
		fmt.Printf("Location set: lat=%s lon=%s\n", lat, lon)
	}
}

func cmdPredictWindow(cfg Config) {
	durStr := flagVal("--duration")
	if durStr == "" {
		fmt.Fprintln(os.Stderr, "usage: pp-starlink predict-window --duration <minutes>")
		os.Exit(1)
	}
	useSynthetic := hasFlag("--synthetic")
	mins, err := strconv.Atoi(durStr)
	if err != nil || mins <= 0 {
		fmt.Fprintf(os.Stderr, "invalid --duration: %s\n", durStr)
		os.Exit(1)
	}

	d := mustDB(cfg.DBPath)
	defer d.Close()

	latStr, err := d.GetConfig("latitude")
	if err == sql.ErrNoRows {
		fmt.Fprintln(os.Stderr, "observer location not set — run: pp-starlink set-location --lat <float> --lon <float>")
		os.Exit(1)
	} else if err != nil {
		log.Fatal(err)
	}
	lonStr, err2 := d.GetConfig("longitude")
	if err2 != nil {
		log.Fatal(err2)
	}
	lat, _ := strconv.ParseFloat(latStr, 64)
	lon, _ := strconv.ParseFloat(lonStr, 64)

	tles, err := orbit.FetchTLEs(cfg.TLECacheFile)
	if err != nil {
		log.Fatalf("TLE fetch: %v", err)
	}
	log.Printf("loaded %d TLEs", len(tles))

	buckets, err := d.SpatialBuckets(cfg.LossThreshold)
	if err != nil {
		log.Fatal(err)
	}

	var bad []orbit.BadZone
	if useSynthetic {
		bad = syntheticBadZones()
		fmt.Println("Using synthetic risk zones for development mode (--synthetic).")
	} else {
		if len(buckets) == 0 {
			fmt.Println("No historical bad zones found — collect more telemetry with the daemon first.")
			fmt.Println("Tip: use --synthetic for development/demo output without historical zones.")
			return
		}
		bad = make([]orbit.BadZone, len(buckets))
		for i, b := range buckets {
			bad[i] = orbit.BadZone{Az: b.AzBucket, El: b.ElBucket}
		}
	}

	windows := orbit.PassesInWindow(tles, lat, lon, bad, time.Duration(mins)*time.Minute)
	if len(windows) == 0 {
		fmt.Printf("No predicted risk windows in the next %d minutes.\n", mins)
		return
	}

	fmt.Printf("## Predicted drop risk windows (next %d min)\n", mins)
	fmt.Println("| Start | End | Satellite | Az (°) | El (°) | Pred MOS | Rationale |")
	fmt.Println("|-------|-----|-----------|--------|--------|----------|-----------|")
	for _, w := range windows {
		mos, rationale := predictWindowMOS(w, buckets, useSynthetic)
		fmt.Printf("| %s | %s | %-20s | %6.1f | %6.1f | %8.2f | %s |\n",
			w.Start.Format("15:04:05"), w.End.Format("15:04:05"),
			w.SatID, w.Azimuth, w.Elevation, mos, rationale)
	}
}

func predictWindowMOS(w orbit.RiskWindow, buckets []db.SpatialBucket, synthetic bool) (float64, string) {
	const (
		azTol = 5.0
		elTol = 2.5
	)

	matched := make([]db.SpatialBucket, 0, 4)
	if len(buckets) > 0 {
		closest := buckets[0]
		bestDist := math.MaxFloat64
		for _, b := range buckets {
			dAz := math.Abs(w.Azimuth - b.AzBucket)
			dEl := math.Abs(w.Elevation - b.ElBucket)
			dist := dAz/azTol + dEl/elTol
			if dist < bestDist {
				bestDist = dist
				closest = b
			}
			if dAz <= azTol && dEl <= elTol {
				matched = append(matched, b)
			}
		}

		if len(matched) == 0 {
			matched = append(matched, closest)
		}
	}

	weightTotal := 0.0
	weightedLoss := 0.0
	evidence := 0
	for _, b := range matched {
		wgt := float64(b.Incidents)
		if wgt < 1 {
			wgt = 1
		}
		weightTotal += wgt
		weightedLoss += b.AvgLoss * wgt
		evidence += b.Incidents
	}
	if weightTotal > 0 {
		weightedLoss /= weightTotal
	}

	windowMins := w.End.Sub(w.Start).Minutes() + 1
	if windowMins < 1 {
		windowMins = 1
	}

	if len(matched) == 0 {
		// Synthetic mode fallback: no historical evidence, rely on geometry + duration.
		weightedLoss = 0.18
	}

	lossRisk := weightedLoss
	evidenceRisk := 0.15 * math.Min(1.0, float64(evidence)/20.0)
	durationRisk := 0.10 * math.Min(1.0, windowMins/5.0)
	elevationRisk := 0.25 * ((90.0 - w.Elevation) / 90.0)
	totalRisk := math.Min(1.0, math.Max(0.0, lossRisk+evidenceRisk+durationRisk+elevationRisk))

	mos := 4.9 - (3.4 * totalRisk)
	if mos < 1.0 {
		mos = 1.0
	}
	if mos > 5.0 {
		mos = 5.0
	}

	rationale := ""
	if synthetic && len(matched) == 0 {
		rationale = fmt.Sprintf(
			"synthetic baseline loss %.0f%%; pass el %.1f deg; window %.0f min",
			weightedLoss*100,
			w.Elevation,
			windowMins,
		)
	} else {
		rationale = fmt.Sprintf(
			"hist loss %.0f%% across %d incidents; pass el %.1f deg; window %.0f min",
			weightedLoss*100,
			evidence,
			w.Elevation,
			windowMins,
		)
	}
	return mos, rationale
}

func syntheticBadZones() []orbit.BadZone {
	return []orbit.BadZone{
		{Az: 330, El: 82},
		{Az: 300, El: 78},
		{Az: 200, El: 76},
		{Az: 80, El: 80},
		{Az: 350, El: 74},
	}
}

func fmtDB(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.1f dB", *v)
}

func fmtSat(id *string, az, el *float64) string {
	if id == nil || az == nil || el == nil {
		return ""
	}
	return fmt.Sprintf(" sat=%s az=%.1f° el=%.1f°", *id, *az, *el)
}

// detectPOP grabs traceroute hop 2 — typically the Starlink POP.
func detectPOP() (string, error) {
	out, err := exec.Command("traceroute", "-m", "3", "-n", "-w", "2", "8.8.8.8").Output()
	if err != nil {
		return "", err
	}
	for i, line := range strings.Split(string(out), "\n") {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if i == 2 && len(fields) >= 2 && fields[1] != "*" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("hop 2 not found")
}
