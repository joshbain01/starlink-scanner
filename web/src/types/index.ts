// ── Starlink dish types mirroring pp-starlink JSON output ─────────────────

export interface Alerts {
  thermal_shutdown: boolean
  thermal_throttle: boolean
  power_supply_thermal_throttle: boolean
  is_heating: boolean
  motors_stuck: boolean
  mast_not_near_vertical: boolean
  slow_ethernet: boolean
  slow_ethernet_100mbps: boolean
  no_ethernet_link: boolean
  dish_water_detected: boolean
  router_water_detected: boolean
  lower_signal_than_predicted: boolean
  roaming: boolean
  unexpected_location: boolean
  install_pending: boolean
  power_save_idle: boolean
  low_motor_current: boolean
  obstruction_map_reset: boolean
  upsu_router_port_slow: boolean
}

export interface StatusResponse {
  dish_id: string
  hardware_version: string
  software_version: string
  bootcount: number
  uptime_s: number
  boresight_azimuth_deg: number
  boresight_elevation_deg: number
  tilt_angle_deg: number
  attitude_uncertainty_deg: number
  is_snr_above_noise_floor: boolean
  is_snr_persistently_low: boolean
  pop_latency_ms: number
  pop_drop_rate: number
  downlink_bps: number
  uplink_bps: number
  eth_speed_mbps: number
  is_cell_disabled: boolean
  dl_bandwidth_restricted_reason: string
  ul_bandwidth_restricted_reason: string
  outage_cause: string
  alerts: string[]
  recent_outages_15min: OutageEvent[]
}

export interface OutageEvent {
  cause: string
  start_timestamp_ns: number
  duration_ms: number
  did_switch: boolean
}

export interface InsightEvent {
  timestamp: string
  packet_loss: number
  beacon_snr?: number
  noise_floor?: number
  baseline_snr?: number
  baseline_noise?: number
  lower_signal_than_predicted: boolean
  is_snr_above_noise_floor: boolean
  cause: string
  satellite_id?: string
  azimuth?: number
  elevation?: number
}

export interface SpatialBucket {
  az_bucket: number
  el_bucket: number
  avg_loss: number
  incidents: number
}

export interface ReportSummary {
  total_samples: number
  first_sample: string
  last_sample: string
  avg_loss: number
  max_loss: number
  avg_obstruction: number
  max_obstruction: number
  avg_local_jitter: number
  max_local_jitter: number
}

export interface DayStat {
  day: string
  samples: number
  drops: number
  avg_loss: number
  avg_obstruction: number
  avg_local_jitter: number
}

export interface HourStat {
  hour: number
  samples: number
  drops: number
  avg_loss: number
}

export interface ReportResponse {
  summary: ReportSummary
  days: DayStat[]
  hours: HourStat[]
  buckets: SpatialBucket[]
}

export interface RiskWindow {
  start: string
  end: string
  sat_id: string
  azimuth: number
  elevation: number
}

// ── Derived / UI types ─────────────────────────────────────────────────────

export type SignalLevel = 'healthy' | 'degraded' | 'critical' | 'unknown'

export function signalLevel(loss: number): SignalLevel {
  if (loss <= 0.02) return 'healthy'
  if (loss <= 0.10) return 'degraded'
  return 'critical'
}

export type DropCause = 'rf-blockage' | 'emi' | 'dish-signal' | 'congestion' | 'unknown'

export function parseCause(cause: string): DropCause {
  if (cause.includes('[RF]') && cause.includes('Blockage')) return 'rf-blockage'
  if (cause.includes('[RF]') && cause.includes('EMI'))      return 'emi'
  if (cause.includes('[dish]'))                             return 'dish-signal'
  if (cause.includes('[!]'))                                return 'congestion'
  return 'unknown'
}
