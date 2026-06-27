import { useEffect, useState } from 'react'
import {
  ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip, CartesianGrid, Cell,
} from 'recharts'
import { api } from '@/api/client'
import type { ReportResponse } from '@/types'
import { PageHeader, Spinner, ErrorMessage, Card, StatCard } from '@/components/ui'

export function ReportView() {
  const [report, setReport] = useState<ReportResponse | null>(null)
  const [error, setError]   = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.report()
      .then(setReport)
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <div className="p-7"><Spinner /></div>
  if (error)   return <div className="p-7"><ErrorMessage message={error} /></div>
  if (!report) return null

  const s = report.summary

  return (
    <div className="p-7">
      <PageHeader title="Report" sub={`${s.first_sample ? new Date(s.first_sample).toLocaleDateString() : '—'} → ${s.last_sample ? new Date(s.last_sample).toLocaleDateString() : '—'}`} />

      {/* Summary stats */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-5">
        <StatCard label="Samples"       value={s.total_samples.toLocaleString()} level="unknown" />
        <StatCard label="Avg loss"      value={`${(s.avg_loss * 100).toFixed(2)}%`} level={s.avg_loss < 0.02 ? 'healthy' : s.avg_loss < 0.1 ? 'degraded' : 'critical'} />
        <StatCard label="Avg jitter"    value={`${s.avg_local_jitter.toFixed(1)} ms`} level={s.avg_local_jitter < 5 ? 'healthy' : s.avg_local_jitter < 15 ? 'degraded' : 'critical'} />
        <StatCard label="Max loss"      value={`${(s.max_loss * 100).toFixed(0)}%`} level={s.max_loss < 0.1 ? 'healthy' : 'critical'} />
      </div>

      {/* Day chart */}
      {report.days.length > 0 && (
        <Card className="p-5 mb-5">
          <div className="text-xs font-semibold text-t2 mb-4">Drop rate by day (%)</div>
          <ResponsiveContainer width="100%" height={160}>
            <BarChart data={report.days.map((d) => ({
              day: d.day.slice(5),
              loss: +(d.avg_loss * 100).toFixed(2),
            }))}>
              <CartesianGrid strokeDasharray="3 3" stroke="#1d3454" />
              <XAxis dataKey="day" tick={{ fill: '#3d6285', fontSize: 10, fontFamily: 'JetBrains Mono' }} />
              <YAxis tick={{ fill: '#3d6285', fontSize: 10, fontFamily: 'JetBrains Mono' }} unit="%" />
              <Tooltip
                contentStyle={{ background: '#0f1e35', border: '1px solid #1d3454', borderRadius: 8, fontSize: 12 }}
                labelStyle={{ color: '#7fa8cc' }}
                itemStyle={{ color: '#00e5a0', fontFamily: 'JetBrains Mono' }}
              />
              <Bar dataKey="loss" radius={[3, 3, 0, 0]}>
                {report.days.map((d, i) => (
                  <Cell key={i} fill={d.avg_loss >= 0.1 ? '#ff4560' : d.avg_loss >= 0.02 ? '#ffb800' : '#00e5a0'} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}

      {/* Hour of day chart */}
      {report.hours.length > 0 && (
        <Card className="p-5">
          <div className="text-xs font-semibold text-t2 mb-4">Drop rate by hour of day (all days combined)</div>
          <ResponsiveContainer width="100%" height={140}>
            <BarChart data={report.hours.map((h) => ({
              hour: `${String(h.hour).padStart(2, '0')}:00`,
              loss: +(h.avg_loss * 100).toFixed(2),
            }))}>
              <CartesianGrid strokeDasharray="3 3" stroke="#1d3454" />
              <XAxis dataKey="hour" tick={{ fill: '#3d6285', fontSize: 9, fontFamily: 'JetBrains Mono' }} />
              <YAxis tick={{ fill: '#3d6285', fontSize: 10, fontFamily: 'JetBrains Mono' }} unit="%" />
              <Tooltip
                contentStyle={{ background: '#0f1e35', border: '1px solid #1d3454', borderRadius: 8, fontSize: 12 }}
                labelStyle={{ color: '#7fa8cc' }}
                itemStyle={{ color: '#00a8ff', fontFamily: 'JetBrains Mono' }}
              />
              <Bar dataKey="loss" fill="#00a8ff" radius={[2, 2, 0, 0]} opacity={0.8} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}
    </div>
  )
}
