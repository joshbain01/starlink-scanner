package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"pp-starlink/internal/analysis"
)

type DB struct{ db *sql.DB }

type NetworkSample struct {
	Timestamp                time.Time
	UptimeS                  uint64
	ObstructionFraction      float32
	CurrentlyObstructed      bool
	ThermalShutdown          bool
	ThermalThrottle          bool
	SlowEthernet             bool
	GatewayJitterMs          float64
	POPJitterMs              float64
	POPLatencyMs             float64
	POPDropRate              float64
	PublicPacketLoss         float64
	LowerSignalThanPredicted bool
	IsSnrAboveNoiseFloor     bool
	IsSnrPersistentlyLow     bool
	DownlinkBps              float32
	UplinkBps                float32
	OutageCause              string
	// Orbital look-angle (nil when observer location is not configured)
	SatelliteID *string
	Azimuth     *float64
	Elevation   *float64

	// Dish physical pointing (from firmware)
	BoresightAzimuthDeg   *float32
	BoresightElevationDeg *float32
	TiltAngleDeg          *float32

	// SpaceX throttle (empty = no limit)
	DLBandwidthRestrictedReason string
	ULBandwidthRestrictedReason string

	// Ethernet
	EthSpeedMbps int32

	// Extended alerts
	IsHeating                  bool
	PowerSupplyThermalThrottle bool
	DishWaterDetected          bool
	RouterWaterDetected        bool
	NoEthernetLink             bool
	Roaming                    bool

	// History-derived window stats (last 15 seconds)
	MaxLatencyMs        float32
	MinLatencyMs        float32
	BriefOutageCount    int
	BriefOutageDurationS float32
}

// SpatialBucket holds aggregated loss statistics for an az/el grid cell.
type SpatialBucket struct {
	AzBucket, ElBucket float64
	AvgLoss            float64
	Incidents          int
}

type InsightEvent struct {
	Timestamp                time.Time
	PacketLoss               float64
	BeaconSNR                *float64
	NoiseFloor               *float64
	BaselineSNR              *float64
	BaselineNoise            *float64
	LowerSignalThanPredicted bool
	IsSnrAboveNoiseFloor     bool
	Cause                    string
	SatelliteID              *string
	Azimuth                  *float64
	Elevation                *float64
}

// OutageRecord is one structured outage event from the dish history buffer.
type OutageRecord struct {
	StartTimestampNs int64
	DurationNs       uint64
	Cause            string
	DidSwitch        bool
}

func Open(path string) (*DB, error) {
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA synchronous = OFF",
		"PRAGMA cache_size = -4000",
		"PRAGMA auto_vacuum = INCREMENTAL",
	} {
		if _, err := raw.Exec(p); err != nil {
			raw.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	d := &DB{db: raw}
	if err := d.initSchema(); err != nil {
		raw.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) initSchema() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS network_telemetry (
			timestamp            INTEGER NOT NULL,
			uptime_s             INTEGER,
			obstruction_fraction REAL,
			alert_flags          INTEGER,
			local_jitter         REAL,
			pop_jitter           REAL,
			public_packet_loss   REAL
		);
		CREATE TABLE IF NOT EXISTS rf_telemetry (
			timestamp      INTEGER NOT NULL,
			beacon_snr_db  REAL,
			noise_floor_db REAL
		);
		CREATE TABLE IF NOT EXISTS app_config (
			key   TEXT UNIQUE NOT NULL,
			value TEXT
		);
		CREATE TABLE IF NOT EXISTS outage_telemetry (
			start_timestamp_ns INTEGER PRIMARY KEY,
			duration_ns        INTEGER,
			cause              TEXT,
			did_switch         INTEGER
		);
		CREATE TABLE IF NOT EXISTS event_log (
			start_timestamp_ns INTEGER PRIMARY KEY,
			severity           TEXT,
			reason             TEXT,
			duration_ns        INTEGER,
			device_id          TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_net_ts    ON network_telemetry(timestamp);
		CREATE INDEX IF NOT EXISTS idx_rf_ts     ON rf_telemetry(timestamp);
		CREATE INDEX IF NOT EXISTS idx_outage_ts ON outage_telemetry(start_timestamp_ns);
		CREATE INDEX IF NOT EXISTS idx_event_ts  ON event_log(start_timestamp_ns);
	`)
	if err != nil {
		return err
	}
	for _, col := range []string{
		"ALTER TABLE network_telemetry ADD COLUMN lower_signal_than_predicted INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN is_snr_above_noise_floor INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN target_satellite_id TEXT",
		"ALTER TABLE network_telemetry ADD COLUMN calculated_azimuth REAL",
		"ALTER TABLE network_telemetry ADD COLUMN calculated_elevation REAL",
		"ALTER TABLE network_telemetry ADD COLUMN pop_latency_ms REAL",
		"ALTER TABLE network_telemetry ADD COLUMN pop_drop_rate REAL",
		"ALTER TABLE network_telemetry ADD COLUMN currently_obstructed INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN outage_cause TEXT",
		"ALTER TABLE network_telemetry ADD COLUMN downlink_bps REAL",
		"ALTER TABLE network_telemetry ADD COLUMN uplink_bps REAL",
		"ALTER TABLE network_telemetry ADD COLUMN is_snr_persistently_low INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN boresight_azimuth_deg REAL",
		"ALTER TABLE network_telemetry ADD COLUMN boresight_elevation_deg REAL",
		"ALTER TABLE network_telemetry ADD COLUMN tilt_angle_deg REAL",
		"ALTER TABLE network_telemetry ADD COLUMN dl_bandwidth_restricted_reason TEXT",
		"ALTER TABLE network_telemetry ADD COLUMN ul_bandwidth_restricted_reason TEXT",
		"ALTER TABLE network_telemetry ADD COLUMN eth_speed_mbps INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN alert_is_heating INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN alert_power_supply_thermal_throttle INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN alert_dish_water_detected INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN alert_router_water_detected INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN alert_no_ethernet_link INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN alert_roaming INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN max_latency_ms REAL",
		"ALTER TABLE network_telemetry ADD COLUMN min_latency_ms REAL",
		"ALTER TABLE network_telemetry ADD COLUMN brief_outage_count INTEGER",
		"ALTER TABLE network_telemetry ADD COLUMN brief_outage_duration_s REAL",
	} {
		if _, err := d.db.Exec(col); err != nil && !isDupColumn(err) {
			return err
		}
	}
	return nil
}

func isDupColumn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column")
}

func (d *DB) WriteNetwork(s NetworkSample) error {
	var flags uint32
	if s.ThermalShutdown {
		flags |= 1
	}
	if s.ThermalThrottle {
		flags |= 2
	}
	if s.SlowEthernet {
		flags |= 4
	}
	var cause *string
	if s.OutageCause != "" && s.OutageCause != "UNKNOWN" {
		cause = &s.OutageCause
	}
	var dlReason, ulReason *string
	if s.DLBandwidthRestrictedReason != "" {
		dlReason = &s.DLBandwidthRestrictedReason
	}
	if s.ULBandwidthRestrictedReason != "" {
		ulReason = &s.ULBandwidthRestrictedReason
	}

	_, err := d.db.Exec(
		`INSERT INTO network_telemetry
			(timestamp, uptime_s, obstruction_fraction, alert_flags,
			 local_jitter, pop_jitter, public_packet_loss,
			 lower_signal_than_predicted, is_snr_above_noise_floor,
			 target_satellite_id, calculated_azimuth, calculated_elevation,
			 pop_latency_ms, pop_drop_rate,
			 currently_obstructed, outage_cause,
			 downlink_bps, uplink_bps, is_snr_persistently_low,
			 boresight_azimuth_deg, boresight_elevation_deg, tilt_angle_deg,
			 dl_bandwidth_restricted_reason, ul_bandwidth_restricted_reason,
			 eth_speed_mbps,
			 alert_is_heating, alert_power_supply_thermal_throttle,
			 alert_dish_water_detected, alert_router_water_detected,
			 alert_no_ethernet_link, alert_roaming,
			 max_latency_ms, min_latency_ms,
			 brief_outage_count, brief_outage_duration_s)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.Timestamp.Unix(), s.UptimeS, s.ObstructionFraction, flags,
		s.GatewayJitterMs, s.POPJitterMs, s.PublicPacketLoss,
		boolInt(s.LowerSignalThanPredicted), boolInt(s.IsSnrAboveNoiseFloor),
		s.SatelliteID, s.Azimuth, s.Elevation,
		s.POPLatencyMs, s.POPDropRate,
		boolInt(s.CurrentlyObstructed), cause,
		s.DownlinkBps, s.UplinkBps, boolInt(s.IsSnrPersistentlyLow),
		s.BoresightAzimuthDeg, s.BoresightElevationDeg, s.TiltAngleDeg,
		dlReason, ulReason,
		s.EthSpeedMbps,
		boolInt(s.IsHeating), boolInt(s.PowerSupplyThermalThrottle),
		boolInt(s.DishWaterDetected), boolInt(s.RouterWaterDetected),
		boolInt(s.NoEthernetLink), boolInt(s.Roaming),
		s.MaxLatencyMs, s.MinLatencyMs,
		s.BriefOutageCount, s.BriefOutageDurationS,
	)
	return err
}

// SetConfig stores a key/value pair in app_config.
func (d *DB) SetConfig(key, value string) error {
	_, err := d.db.Exec(`INSERT OR REPLACE INTO app_config (key, value) VALUES (?,?)`, key, value)
	return err
}

// GetConfig retrieves a value from app_config; returns sql.ErrNoRows if missing.
func (d *DB) GetConfig(key string) (string, error) {
	var v string
	err := d.db.QueryRow(`SELECT value FROM app_config WHERE key = ?`, key).Scan(&v)
	return v, err
}

const spatialSQL = `
SELECT
	ROUND(calculated_azimuth  / 10.0) * 10.0 AS az_bucket,
	ROUND(calculated_elevation /  5.0) *  5.0 AS el_bucket,
	AVG(public_packet_loss)                    AS avg_loss,
	COUNT(*)                                   AS drop_incidents
FROM network_telemetry
WHERE public_packet_loss > ? AND calculated_azimuth IS NOT NULL
GROUP BY az_bucket, el_bucket
ORDER BY avg_loss DESC
`

// SpatialBuckets returns az/el grid cells ranked by average packet loss.
func (d *DB) SpatialBuckets(lossThreshold float64) ([]SpatialBucket, error) {
	rows, err := d.db.Query(spatialSQL, lossThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpatialBucket
	for rows.Next() {
		var b SpatialBucket
		if err := rows.Scan(&b.AzBucket, &b.ElBucket, &b.AvgLoss, &b.Incidents); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// WriteOutageEvents inserts structured outage events from the history buffer.
// Uses INSERT OR IGNORE so repeated calls on the same events are safe.
func (d *DB) WriteOutageEvents(events []OutageRecord) error {
	for _, e := range events {
		_, err := d.db.Exec(
			`INSERT OR IGNORE INTO outage_telemetry
				(start_timestamp_ns, duration_ns, cause, did_switch)
			VALUES (?,?,?,?)`,
			e.StartTimestampNs, e.DurationNs, e.Cause, boolInt(e.DidSwitch),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) WriteRF(ts time.Time, snrDB, noiseDB float64) error {
	_, err := d.db.Exec(
		`INSERT INTO rf_telemetry VALUES (?,?,?)`,
		ts.Unix(), snrDB, noiseDB,
	)
	return err
}

// Prune deletes rows older than 30 days. Call at most once per day — do not
// call on every daemon tick, as repeated DELETEs cause unnecessary flash wear.
func (d *DB) Prune() error {
	cutoff30 := time.Now().AddDate(0, 0, -30).Unix()
	if _, err := d.db.Exec(`DELETE FROM network_telemetry WHERE timestamp < ?`, cutoff30); err != nil {
		return err
	}
	if _, err := d.db.Exec(`DELETE FROM rf_telemetry WHERE timestamp < ?`, cutoff30); err != nil {
		return err
	}
	// outage_telemetry and event_log use nanosecond timestamps; 7-day retention.
	cutoff7Ns := time.Now().AddDate(0, 0, -7).UnixNano()
	if _, err := d.db.Exec(`DELETE FROM outage_telemetry WHERE start_timestamp_ns < ?`, cutoff7Ns); err != nil {
		return err
	}
	_, err := d.db.Exec(`DELETE FROM event_log WHERE start_timestamp_ns < ?`, cutoff7Ns)
	return err
}

// Vacuum reclaims pages freed by Prune. Call at most once per week — each
// call rewrites freed pages to disk and should not run on every tick.
func (d *DB) Vacuum() error {
	_, err := d.db.Exec(`PRAGMA incremental_vacuum(500)`)
	return err
}

const insightsSQL = `
SELECT
	n.timestamp,
	n.public_packet_loss,
	r.beacon_snr_db,
	r.noise_floor_db,
	(SELECT AVG(beacon_snr_db)  FROM rf_telemetry WHERE timestamp > strftime('%s','now') - 86400) AS baseline_snr,
	(SELECT AVG(noise_floor_db) FROM rf_telemetry WHERE timestamp > strftime('%s','now') - 86400) AS baseline_noise,
	n.lower_signal_than_predicted,
	n.is_snr_above_noise_floor,
	n.target_satellite_id,
	n.calculated_azimuth,
	n.calculated_elevation
FROM network_telemetry n
LEFT JOIN rf_telemetry r ON ABS(r.timestamp - n.timestamp) <= 1
WHERE n.public_packet_loss > ?
ORDER BY n.timestamp DESC
LIMIT 100
`

func (d *DB) QueryInsights(lossThreshold, snrDelta, noiseDelta float64) ([]InsightEvent, error) {
	rows, err := d.db.Query(insightsSQL, lossThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []InsightEvent
	for rows.Next() {
		var (
			ts                         int64
			loss                       float64
			snr, noise                 *float64
			bsnr, bnoise               *float64
			lowerSignal, snrAboveNoise *int64
			satID                      *string
			az, el                     *float64
		)
		if err := rows.Scan(&ts, &loss, &snr, &noise, &bsnr, &bnoise, &lowerSignal, &snrAboveNoise, &satID, &az, &el); err != nil {
			return nil, err
		}
		lower := lowerSignal != nil && *lowerSignal == 1
		aboveNoise := snrAboveNoise == nil || *snrAboveNoise == 1
		events = append(events, InsightEvent{
			Timestamp:                time.Unix(ts, 0),
			PacketLoss:               loss,
			BeaconSNR:                snr,
			NoiseFloor:               noise,
			BaselineSNR:              bsnr,
			BaselineNoise:            bnoise,
			LowerSignalThanPredicted: lower,
			IsSnrAboveNoiseFloor:     aboveNoise,
			Cause: analysis.DiagnoseCause(analysis.DiagnoseParams{
				BeaconSNR:                snr,
				BaselineSNR:              bsnr,
				NoiseFloor:               noise,
				BaselineNoise:            bnoise,
				LowerSignalThanPredicted: lower,
				IsSnrAboveNoiseFloor:     aboveNoise,
				SNRDelta:                 snrDelta,
				NoiseDelta:               noiseDelta,
			}),
			SatelliteID:              satID,
			Azimuth:                  az,
			Elevation:                el,
		})
	}
	return events, rows.Err()
}

// ObstructionZone is one az/el bucket from the physical obstruction map.
type ObstructionZone struct {
	AzBucket  float64
	ElBucket  float64
	Incidents int
	AvgLoss   float64
}

// QueryObstructionMap returns az/el zones where the dish itself flagged
// currently_obstructed=true, ranked by incident count. These are the physical
// directions to inspect — unlike SpatialBuckets (which maps network drops to
// satellite positions), these are confirmed sky blockages reported by the dish.
func (d *DB) QueryObstructionMap() ([]ObstructionZone, error) {
	rows, err := d.db.Query(`
		SELECT
			ROUND(calculated_azimuth  / 10.0) * 10.0 AS az,
			ROUND(calculated_elevation /  5.0) *  5.0 AS el,
			COUNT(*)                                    AS incidents,
			AVG(public_packet_loss)                     AS avg_loss
		FROM network_telemetry
		WHERE currently_obstructed = 1
		  AND calculated_azimuth  IS NOT NULL
		  AND calculated_elevation IS NOT NULL
		GROUP BY az, el
		ORDER BY incidents DESC, avg_loss DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ObstructionZone
	for rows.Next() {
		var z ObstructionZone
		if err := rows.Scan(&z.AzBucket, &z.ElBucket, &z.Incidents, &z.AvgLoss); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// ReportSummary holds the top-level stats returned by QueryReport.
type ReportSummary struct {
	TotalSamples   int
	FirstSample    time.Time
	LastSample     time.Time
	AvgLoss        float64
	MaxLoss        float64
	AvgObstruction float64
	MaxObstruction float64
	AvgLocalJitter float64
	MaxLocalJitter float64
}

// DayStat holds per-day aggregates.
type DayStat struct {
	Day            string
	Samples        int
	Drops          int
	AvgLoss        float64
	AvgObstruction float64
	AvgLocalJitter float64
}

// HourStat holds drop rate for one hour-of-day slot (all days combined).
type HourStat struct {
	Hour    int
	Samples int
	Drops   int
	AvgLoss float64
}

// OutageBurst is a run of consecutive 100%-loss samples.
type OutageBurst struct {
	Start    time.Time
	End      time.Time
	Samples  int
	Duration time.Duration
}

// RebootEvent is a moment where dish uptime reset.
type RebootEvent struct {
	At        time.Time
	NewUptime uint64
}

// CauseStat is one row of the outage-cause breakdown.
type CauseStat struct {
	Cause    string
	Samples  int
	AvgLoss  float64
}

// HandoffStat compares drop rates at handoff vs non-handoff samples.
type HandoffStat struct {
	HandoffSamples    int
	HandoffDrops      int
	NonHandoffSamples int
	NonHandoffDrops   int
}

func (d *DB) queryReportSummary() (ReportSummary, error) {
	var s ReportSummary
	var first, last int64
	err := d.db.QueryRow(`
		SELECT COUNT(*), MIN(timestamp), MAX(timestamp),
		       AVG(public_packet_loss), MAX(public_packet_loss),
		       AVG(obstruction_fraction), MAX(obstruction_fraction),
		       AVG(local_jitter), MAX(local_jitter)
		FROM network_telemetry
	`).Scan(&s.TotalSamples, &first, &last,
		&s.AvgLoss, &s.MaxLoss,
		&s.AvgObstruction, &s.MaxObstruction,
		&s.AvgLocalJitter, &s.MaxLocalJitter)
	if err != nil {
		return s, err
	}
	s.FirstSample = time.Unix(first, 0)
	s.LastSample = time.Unix(last, 0)
	return s, nil
}

func (d *DB) queryDayStats(lossThreshold float64) ([]DayStat, error) {
	rows, err := d.db.Query(`
		SELECT date(timestamp,'unixepoch','localtime'),
		       COUNT(*),
		       SUM(CASE WHEN public_packet_loss >= ? THEN 1 ELSE 0 END),
		       AVG(public_packet_loss),
		       AVG(obstruction_fraction),
		       AVG(local_jitter)
		FROM network_telemetry
		GROUP BY 1 ORDER BY 1
	`, lossThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DayStat
	for rows.Next() {
		var s DayStat
		if err := rows.Scan(&s.Day, &s.Samples, &s.Drops, &s.AvgLoss, &s.AvgObstruction, &s.AvgLocalJitter); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) queryHourStats(lossThreshold float64) ([]HourStat, error) {
	rows, err := d.db.Query(`
		SELECT CAST(strftime('%H', timestamp, 'unixepoch', 'localtime') AS INTEGER),
		       COUNT(*),
		       SUM(CASE WHEN public_packet_loss >= ? THEN 1 ELSE 0 END),
		       AVG(public_packet_loss)
		FROM network_telemetry
		GROUP BY 1 ORDER BY 1
	`, lossThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HourStat
	for rows.Next() {
		var s HourStat
		if err := rows.Scan(&s.Hour, &s.Samples, &s.Drops, &s.AvgLoss); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// BurstSummary aggregates all outage bursts into a single summary plus the worst few.
type BurstSummary struct {
	TotalBursts     int
	TotalOutageTime time.Duration
	Worst           []OutageBurst // top 5 by duration
}

func (d *DB) queryOutageBursts() (BurstSummary, error) {
	rows, err := d.db.Query(`
		SELECT MIN(timestamp), MAX(timestamp), COUNT(*)
		FROM (
			SELECT timestamp,
			       (timestamp - ROW_NUMBER() OVER (ORDER BY timestamp) * 15) AS grp
			FROM network_telemetry
			WHERE public_packet_loss >= 1.0
		)
		GROUP BY grp
		HAVING COUNT(*) >= 2
		ORDER BY (MAX(timestamp) - MIN(timestamp)) DESC
	`)
	if err != nil {
		return BurstSummary{}, err
	}
	defer rows.Close()
	var s BurstSummary
	for rows.Next() {
		var start, end int64
		var n int
		if err := rows.Scan(&start, &end, &n); err != nil {
			return s, err
		}
		dur := time.Duration(end-start) * time.Second
		s.TotalBursts++
		s.TotalOutageTime += dur
		if len(s.Worst) < 5 {
			s.Worst = append(s.Worst, OutageBurst{
				Start:    time.Unix(start, 0),
				End:      time.Unix(end, 0),
				Samples:  n,
				Duration: dur,
			})
		}
	}
	return s, rows.Err()
}

func (d *DB) queryReboots() ([]RebootEvent, error) {
	rows, err := d.db.Query(`
		SELECT timestamp, uptime_s
		FROM (
			SELECT timestamp, uptime_s,
			       LAG(uptime_s) OVER (ORDER BY timestamp) AS prev
			FROM network_telemetry
		)
		WHERE prev IS NOT NULL AND uptime_s < prev
		ORDER BY timestamp
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RebootEvent
	for rows.Next() {
		var ts int64
		var u uint64
		if err := rows.Scan(&ts, &u); err != nil {
			return nil, err
		}
		out = append(out, RebootEvent{At: time.Unix(ts, 0), NewUptime: u})
	}
	return out, rows.Err()
}

func (d *DB) queryCauseStats(lossThreshold float64) ([]CauseStat, error) {
	// Only return rows where the dish actually reported a cause — exclude the
	// NULL/unclassified rows that exist only because data predates this feature.
	rows, err := d.db.Query(`
		SELECT outage_cause AS cause,
		       COUNT(*) AS n,
		       AVG(public_packet_loss) AS avg_loss
		FROM network_telemetry
		WHERE public_packet_loss >= ?
		  AND outage_cause IS NOT NULL
		GROUP BY cause
		ORDER BY n DESC
	`, lossThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CauseStat
	for rows.Next() {
		var s CauseStat
		if err := rows.Scan(&s.Cause, &s.Samples, &s.AvgLoss); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SatelliteStat is per-satellite drop aggregates.
type SatelliteStat struct {
	SatID     string
	Incidents int
	AvgLoss   float64
	MaxLoss   float64
}

// QueryTroubleSatellites returns the satellites most often overhead during drop
// events, ordered by incident count then avg loss. totalDropSatellites is the
// total number of distinct satellites that had any drop — useful for detecting
// whether the problem is per-satellite or systemic.
func (d *DB) queryTroubleSatellites(lossThreshold float64, limit int) (stats []SatelliteStat, totalDistinct int, err error) {
	err = d.db.QueryRow(`
		SELECT COUNT(DISTINCT target_satellite_id)
		FROM network_telemetry
		WHERE public_packet_loss >= ? AND target_satellite_id IS NOT NULL
	`, lossThreshold).Scan(&totalDistinct)
	if err != nil {
		return
	}
	rows, err := d.db.Query(`
		SELECT target_satellite_id, COUNT(*), AVG(public_packet_loss), MAX(public_packet_loss)
		FROM network_telemetry
		WHERE public_packet_loss >= ? AND target_satellite_id IS NOT NULL
		GROUP BY target_satellite_id
		ORDER BY COUNT(*) DESC, AVG(public_packet_loss) DESC
		LIMIT ?
	`, lossThreshold, limit)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var s SatelliteStat
		if err = rows.Scan(&s.SatID, &s.Incidents, &s.AvgLoss, &s.MaxLoss); err != nil {
			return
		}
		stats = append(stats, s)
	}
	err = rows.Err()
	return
}

func (d *DB) queryHandoffStats(lossThreshold float64) (HandoffStat, error) {
	var s HandoffStat
	err := d.db.QueryRow(`
		WITH ordered AS (
			SELECT public_packet_loss,
			       target_satellite_id,
			       LAG(target_satellite_id) OVER (ORDER BY timestamp) AS prev_sat
			FROM network_telemetry
			WHERE target_satellite_id IS NOT NULL
		)
		SELECT
			SUM(CASE WHEN prev_sat != target_satellite_id THEN 1 ELSE 0 END),
			SUM(CASE WHEN prev_sat != target_satellite_id AND public_packet_loss >= ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN prev_sat = target_satellite_id THEN 1 ELSE 0 END),
			SUM(CASE WHEN prev_sat = target_satellite_id AND public_packet_loss >= ? THEN 1 ELSE 0 END)
		FROM ordered WHERE prev_sat IS NOT NULL
	`, lossThreshold, lossThreshold).Scan(
		&s.HandoffSamples, &s.HandoffDrops,
		&s.NonHandoffSamples, &s.NonHandoffDrops)
	return s, err
}

// ReportParams controls what QueryReport assembles.
type ReportParams struct {
	LossThreshold float64
	SatLimit      int // max rows in Report.Satellites
}

// Report is the full telemetry report returned by QueryReport.
type Report struct {
	Summary       ReportSummary
	Days          []DayStat
	Hours         []HourStat
	Bursts        BurstSummary
	Reboots       []RebootEvent
	Causes        []CauseStat
	Satellites    []SatelliteStat
	TotalSatDrops int
	Handoffs      HandoffStat
	Buckets       []SpatialBucket
}

// QueryReport assembles all report data in one call. The nine underlying
// query methods are private implementation details; callers receive a Report.
func (d *DB) QueryReport(p ReportParams) (*Report, error) {
	r := &Report{}
	var err error
	if r.Summary, err = d.queryReportSummary(); err != nil {
		return nil, fmt.Errorf("summary: %w", err)
	}
	if r.Days, err = d.queryDayStats(p.LossThreshold); err != nil {
		return nil, fmt.Errorf("day stats: %w", err)
	}
	if r.Hours, err = d.queryHourStats(p.LossThreshold); err != nil {
		return nil, fmt.Errorf("hour stats: %w", err)
	}
	if r.Bursts, err = d.queryOutageBursts(); err != nil {
		return nil, fmt.Errorf("outage bursts: %w", err)
	}
	if r.Reboots, err = d.queryReboots(); err != nil {
		return nil, fmt.Errorf("reboots: %w", err)
	}
	if r.Causes, err = d.queryCauseStats(p.LossThreshold); err != nil {
		return nil, fmt.Errorf("cause stats: %w", err)
	}
	if r.Satellites, r.TotalSatDrops, err = d.queryTroubleSatellites(p.LossThreshold, p.SatLimit); err != nil {
		return nil, fmt.Errorf("trouble satellites: %w", err)
	}
	if r.Handoffs, err = d.queryHandoffStats(p.LossThreshold); err != nil {
		return nil, fmt.Errorf("handoff stats: %w", err)
	}
	if r.Buckets, err = d.SpatialBuckets(p.LossThreshold); err != nil {
		return nil, fmt.Errorf("spatial buckets: %w", err)
	}
	return r, nil
}
