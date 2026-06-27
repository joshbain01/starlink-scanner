import { useEffect, useState } from 'react'
import { api } from '@/api/client'
import type { StatusResponse } from '@/types'
import { signalLevel } from '@/types'
import {
  StatCard, Badge, LiveIndicator, SignalBars, PageHeader, Spinner, ErrorMessage, Card,
} from '@/components/ui'

export function StatusView() {
  const [data, setData]   = useState<StatusResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  async function load() {
    try {
      setData(await api.status())
      setError(null)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const t = setInterval(load, 15_000)
    return () => clearInterval(t)
  }, [])

  if (loading) return <div className="p-7"><Spinner /></div>
  if (error)   return <div className="p-7"><ErrorMessage message={error} /></div>
  if (!data)   return null

  const lossLevel = signalLevel(data.pop_drop_rate)
  const latLevel  = data.pop_latency_ms < 50 ? 'healthy' : data.pop_latency_ms < 120 ? 'degraded' : 'critical'
  const obsLevel  = data.alerts.includes('obstruction_map_reset') ? 'degraded' : 'unknown'
  const uptimeH   = (data.uptime_s / 3600).toFixed(1)
  const dlMbps    = (data.downlink_bps / 1e6).toFixed(0)
  const ulMbps    = (data.uplink_bps  / 1e6).toFixed(0)

  return (
    <div className="p-7">
      <PageHeader
        title="Dish Status"
        sub={`${data.dish_id} · fw ${data.software_version}`}
        live
        actions={
          <button
            onClick={load}
            className="px-3 py-1.5 text-xs font-semibold border border-border-bright rounded-md text-t2 hover:bg-orbit hover:text-t1 transition-colors"
          >
            Refresh
          </button>
        }
      />

      {/* Primary metrics */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3 mb-6">
        <StatCard label="Uptime"      value={`${uptimeH}h`}         sub={`${data.bootcount} reboots`}   level="healthy" />
        <StatCard label="POP Loss"    value={`${(data.pop_drop_rate * 100).toFixed(1)}%`} sub="Dish-reported" level={lossLevel} />
        <StatCard label="POP Latency" value={`${data.pop_latency_ms.toFixed(0)} ms`}  sub="Starlink POP"  level={latLevel} />
        <StatCard label="Throughput ↓" value={`${dlMbps} Mbps`}     sub="Downlink"     level="unknown" />
        <StatCard label="Throughput ↑" value={`${ulMbps} Mbps`}     sub="Uplink"       level="unknown" />
        <StatCard label="Ethernet"    value={`${data.eth_speed_mbps} Mbps`}            sub="LAN speed"    level={data.eth_speed_mbps >= 100 ? 'healthy' : 'degraded'} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Pointing */}
        <Card className="p-5">
          <div className="text-xs font-semibold text-t2 mb-4">Dish Pointing</div>
          <div className="grid grid-cols-2 gap-4">
            <Metric label="Boresight Az" value={`${data.boresight_azimuth_deg.toFixed(1)}°`} />
            <Metric label="Boresight El" value={`${data.boresight_elevation_deg.toFixed(1)}°`} />
            <Metric label="Tilt Angle"   value={`${data.tilt_angle_deg.toFixed(1)}°`} />
            <Metric label="Uncertainty"  value={`${data.attitude_uncertainty_deg.toFixed(1)}°`} />
          </div>
          <div className="mt-4 pt-4 border-t border-border-dim">
            <div className="text-xs text-t3 mb-1">Signal quality</div>
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-2">
                <SignalBars level={data.is_snr_above_noise_floor ? 'healthy' : 'critical'} />
                <span className="text-xs text-t2">SNR {data.is_snr_above_noise_floor ? 'above' : 'below'} noise floor</span>
              </div>
              {data.is_snr_persistently_low && (
                <Badge color="amber">Persistently low SNR</Badge>
              )}
            </div>
          </div>
        </Card>

        {/* Alerts */}
        <Card className="p-5">
          <div className="text-xs font-semibold text-t2 mb-4">Active Alerts</div>
          {data.alerts.length === 0 ? (
            <div className="flex items-center gap-2">
              <Badge color="green">All clear</Badge>
              <span className="text-xs text-t3">No alerts active</span>
            </div>
          ) : (
            <div className="flex flex-wrap gap-2">
              {data.alerts.map((a) => (
                <Badge key={a} color={a.includes('thermal') || a.includes('shutdown') ? 'red' : 'amber'}>
                  {a.replace(/_/g, ' ')}
                </Badge>
              ))}
            </div>
          )}

          {/* Throttle reasons */}
          {(data.dl_bandwidth_restricted_reason || data.ul_bandwidth_restricted_reason) && (
            <div className="mt-4 pt-4 border-t border-border-dim">
              <div className="text-xs text-t3 mb-2">Bandwidth throttle</div>
              {data.dl_bandwidth_restricted_reason && (
                <div className="text-xs"><span className="text-t3">DL:</span> <span className="text-amber">{data.dl_bandwidth_restricted_reason}</span></div>
              )}
              {data.ul_bandwidth_restricted_reason && (
                <div className="text-xs mt-1"><span className="text-t3">UL:</span> <span className="text-amber">{data.ul_bandwidth_restricted_reason}</span></div>
              )}
            </div>
          )}
        </Card>

        {/* Recent outages */}
        {data.recent_outages_15min.length > 0 && (
          <Card className="p-5 lg:col-span-2">
            <div className="text-xs font-semibold text-t2 mb-3">Recent Outages (15 min history)</div>
            <div className="space-y-2">
              {data.recent_outages_15min.slice(0, 6).map((o, i) => (
                <div key={i} className="flex items-center justify-between text-xs border-b border-border-dim pb-2 last:border-0 last:pb-0">
                  <span className="font-mono text-t3">{new Date(o.start_timestamp_ns / 1e6).toLocaleTimeString()}</span>
                  <span className="text-t2">{o.cause}</span>
                  <span className="font-mono text-t1">{o.duration_ms.toFixed(0)} ms</span>
                  {o.did_switch && <Badge color="blue">Handoff</Badge>}
                </div>
              ))}
            </div>
          </Card>
        )}
      </div>
    </div>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[10px] uppercase tracking-wider text-t3 mb-0.5">{label}</div>
      <div className="font-mono text-sm text-t1">{value}</div>
    </div>
  )
}
