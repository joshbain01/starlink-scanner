import { useEffect, useState } from 'react'
import {
  ResponsiveContainer, AreaChart, Area, XAxis, YAxis, Tooltip, CartesianGrid,
} from 'recharts'
import { api } from '@/api/client'
import type { InsightEvent } from '@/types'
import { parseCause } from '@/types'
import {
  CauseBadge, PageHeader, Spinner, ErrorMessage, Card,
} from '@/components/ui'

export function InsightsView() {
  const [events, setEvents] = useState<InsightEvent[]>([])
  const [error, setError]   = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.insights()
      .then(setEvents)
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <div className="p-7"><Spinner /></div>
  if (error)   return <div className="p-7"><ErrorMessage message={error} /></div>

  const chartData = [...events].reverse().map((e) => ({
    time: new Date(e.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    loss: +(e.packet_loss * 100).toFixed(1),
  }))

  return (
    <div className="p-7">
      <PageHeader title="Insights" sub="Drop cause analysis · last 30 days" />

      {/* Loss timeline chart */}
      <Card className="p-5 mb-5">
        <div className="text-xs font-semibold text-t2 mb-4">Packet loss over time (%)</div>
        <ResponsiveContainer width="100%" height={160}>
          <AreaChart data={chartData}>
            <defs>
              <linearGradient id="lossGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%"   stopColor="#ff4560" stopOpacity={0.3} />
                <stop offset="100%" stopColor="#ff4560" stopOpacity={0}   />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke="#1d3454" />
            <XAxis dataKey="time" tick={{ fill: '#3d6285', fontSize: 10, fontFamily: 'JetBrains Mono' }} />
            <YAxis tick={{ fill: '#3d6285', fontSize: 10, fontFamily: 'JetBrains Mono' }} unit="%" />
            <Tooltip
              contentStyle={{ background: '#0f1e35', border: '1px solid #1d3454', borderRadius: 8, fontSize: 12 }}
              labelStyle={{ color: '#7fa8cc' }}
              itemStyle={{ color: '#ff4560', fontFamily: 'JetBrains Mono' }}
            />
            <Area type="monotone" dataKey="loss" stroke="#ff4560" fill="url(#lossGrad)" strokeWidth={1.5} />
          </AreaChart>
        </ResponsiveContainer>
      </Card>

      {/* Event list */}
      {events.length === 0 ? (
        <Card className="p-8 text-center text-sm text-t3">No drop events above threshold in the last 30 days.</Card>
      ) : (
        <Card>
          {events.map((e, i) => <EventRow key={i} event={e} />)}
        </Card>
      )}
    </div>
  )
}

function EventRow({ event: e }: { event: InsightEvent }) {
  const cause = parseCause(e.cause)
  const lossColor = e.packet_loss >= 0.5 ? 'text-danger' : e.packet_loss >= 0.1 ? 'text-amber' : 'text-t2'

  return (
    <div className="flex items-start gap-3 px-5 py-3.5 border-b border-border-dim last:border-0">
      <div className="font-mono text-[11px] text-t3 min-w-[100px] pt-0.5">
        {new Date(e.timestamp).toLocaleTimeString()}
      </div>
      <div className="flex-1 min-w-0">
        <div className="font-mono text-sm mb-1">
          loss <span className={lossColor}>{(e.packet_loss * 100).toFixed(0)}%</span>
          {e.satellite_id && (
            <span className="text-t3"> · {e.satellite_id} az={e.azimuth?.toFixed(1)}° el={e.elevation?.toFixed(1)}°</span>
          )}
        </div>
        <div className="text-xs text-t2 flex flex-wrap items-center gap-2">
          {e.beacon_snr != null && (
            <span>snr={e.beacon_snr.toFixed(1)} dB{e.baseline_snr != null ? ` (baseline ${e.baseline_snr.toFixed(1)})` : ''}</span>
          )}
          {e.noise_floor != null && <span>noise={e.noise_floor.toFixed(1)} dBm</span>}
          <CauseBadge cause={cause} />
        </div>
      </div>
    </div>
  )
}
