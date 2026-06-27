import { useEffect, useState } from 'react'
import { api } from '@/api/client'
import type { SpatialBucket } from '@/types'
import { PageHeader, Spinner, ErrorMessage, Card } from '@/components/ui'

const RADIUS = 110 // SVG compass radius
const CX = 130
const CY = 130

export function ObstructionView() {
  const [buckets, setBuckets] = useState<SpatialBucket[]>([])
  const [error, setError]     = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.buckets()
      .then(setBuckets)
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <div className="p-7"><Spinner /></div>
  if (error)   return <div className="p-7"><ErrorMessage message={error} /></div>

  return (
    <div className="p-7">
      <PageHeader title="Obstruction Map" sub="Az/El drop zones · satellite look-angles" />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
        <Card className="p-6 flex flex-col items-center">
          <div className="text-xs font-semibold text-t2 mb-4 self-start">Sky compass</div>
          <SkyCompass buckets={buckets} />
          <div className="flex gap-4 mt-4 text-xs text-t3">
            <span><span className="text-danger">●</span> &gt;50% avg loss</span>
            <span><span className="text-amber">●</span> 20–50%</span>
            <span><span className="text-t3">·</span> rings = 30° / 60° el</span>
          </div>
        </Card>

        <Card>
          <div className="px-5 py-4 text-xs font-semibold text-t2 border-b border-border-dim">Ranked zones</div>
          {buckets.length === 0 ? (
            <div className="p-6 text-sm text-t3 text-center">No drop zones detected yet — collect more telemetry.</div>
          ) : (
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border-dim">
                  {['Az (°)', 'El (°)', 'Avg loss', 'Incidents'].map((h) => (
                    <th key={h} className="px-4 py-2 text-left font-medium text-t3 uppercase tracking-wider">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {buckets.map((b, i) => (
                  <tr key={i} className="border-b border-border-dim last:border-0 hover:bg-orbit transition-colors">
                    <td className="px-4 py-2.5 font-mono text-t1">{b.az_bucket.toFixed(0)}</td>
                    <td className="px-4 py-2.5 font-mono text-t1">{b.el_bucket.toFixed(0)}</td>
                    <td className="px-4 py-2.5 font-mono">
                      <span className={b.avg_loss >= 0.5 ? 'text-danger' : b.avg_loss >= 0.2 ? 'text-amber' : 'text-green'}>
                        {(b.avg_loss * 100).toFixed(1)}%
                      </span>
                    </td>
                    <td className="px-4 py-2.5 font-mono text-t2">{b.incidents}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </div>
    </div>
  )
}

function SkyCompass({ buckets }: { buckets: SpatialBucket[] }) {
  function dropColor(loss: number): string {
    if (loss >= 0.9) return '#ff4560'
    if (loss >= 0.5) return '#ff4560'
    if (loss >= 0.2) return '#ffb800'
    return '#7fa8cc'
  }

  function zonePoint(az: number, el: number) {
    const lineLen = ((90 - el) / 90) * RADIUS
    const rad = (az * Math.PI) / 180
    return {
      x: CX + lineLen * Math.sin(rad),
      y: CY - lineLen * Math.cos(rad),
      len: lineLen,
    }
  }

  return (
    <svg width="260" height="260" viewBox="0 0 260 260">
      {/* Rings at el=60, el=30 */}
      {[30, 60].map((el) => (
        <circle
          key={el}
          cx={CX} cy={CY}
          r={(90 - el) / 90 * RADIUS}
          fill="none" stroke="#1d3454" strokeWidth="1" strokeDasharray="4 3"
        />
      ))}
      {/* Horizon ring */}
      <circle cx={CX} cy={CY} r={RADIUS} fill="none" stroke="#1d3454" strokeWidth="1" />
      {/* Crosshairs */}
      <line x1={CX} y1={CY - RADIUS} x2={CX} y2={CY + RADIUS} stroke="#1d3454" strokeWidth="1" opacity="0.4" />
      <line x1={CX - RADIUS} y1={CY} x2={CX + RADIUS} y2={CY} stroke="#1d3454" strokeWidth="1" opacity="0.4" />
      {/* Center */}
      <circle cx={CX} cy={CY} r="3" fill="#3d6285" />

      {/* Drop zones */}
      {buckets.filter((b) => b.incidents >= 2).slice(0, 24).map((b, i) => {
        const p = zonePoint(b.az_bucket, b.el_bucket)
        const color = dropColor(b.avg_loss)
        const r = 4 + Math.min(b.incidents / 10, 1) * 6
        return (
          <g key={i}>
            <line x1={CX} y1={CY} x2={p.x} y2={p.y} stroke={color} strokeWidth="1.5" strokeOpacity="0.5" strokeLinecap="round" />
            <circle cx={p.x} cy={p.y} r={r} fill={color} fillOpacity="0.7" />
          </g>
        )
      })}

      {/* Cardinal labels */}
      {[
        { label: 'N', x: CX,           y: CY - RADIUS - 12 },
        { label: 'S', x: CX,           y: CY + RADIUS + 16 },
        { label: 'E', x: CX + RADIUS + 12, y: CY + 4 },
        { label: 'W', x: CX - RADIUS - 8,  y: CY + 4 },
      ].map(({ label, x, y }) => (
        <text key={label} x={x} y={y} fill="#3d6285" fontSize="11" textAnchor="middle" fontFamily="JetBrains Mono,monospace" fontWeight="600">
          {label}
        </text>
      ))}
      {/* El labels */}
      <text x={CX + (90-60)/90*RADIUS + 4} y={CY - 3} fill="#3d6285" fontSize="9" fontFamily="JetBrains Mono,monospace">60°</text>
      <text x={CX + (90-30)/90*RADIUS + 4} y={CY - 3} fill="#3d6285" fontSize="9" fontFamily="JetBrains Mono,monospace">30°</text>
    </svg>
  )
}
