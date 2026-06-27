import { useState } from 'react'
import { api } from '@/api/client'
import type { RiskWindow } from '@/types'
import { PageHeader, Spinner, ErrorMessage, Card, Badge } from '@/components/ui'

export function PredictView() {
  const [windows, setWindows] = useState<RiskWindow[]>([])
  const [error, setError]     = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [duration, setDuration] = useState(60)
  const [ran, setRan]          = useState(false)

  async function run() {
    setLoading(true)
    setError(null)
    try {
      setWindows(await api.predict(duration))
      setRan(true)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="p-7">
      <PageHeader title="Predict Window" sub="Forecast satellite passes through historically lossy zones" />

      <Card className="p-5 mb-5">
        <div className="text-xs font-semibold text-t2 mb-4">Parameters</div>
        <div className="flex items-end gap-4">
          <div>
            <label className="block text-[11px] text-t3 uppercase tracking-wider mb-1">Duration (minutes)</label>
            <input
              type="number"
              min={5} max={480}
              value={duration}
              onChange={(e) => setDuration(+e.target.value)}
              className="glow-focus bg-orbit border border-border-dim rounded-md px-3 py-2 text-sm font-mono text-t1 w-32 outline-none"
            />
          </div>
          <button
            onClick={run}
            disabled={loading}
            className="px-4 py-2 bg-blue text-[#000e1f] text-sm font-semibold rounded-md hover:bg-[#33bbff] disabled:opacity-50 transition-colors"
          >
            {loading ? 'Running…' : 'Run prediction'}
          </button>
        </div>
      </Card>

      {error && <ErrorMessage message={error} />}
      {loading && <Spinner />}

      {ran && !loading && (
        windows.length === 0 ? (
          <Card className="p-8 text-center text-sm text-t3">
            No predicted risk windows in the next {duration} minutes.
          </Card>
        ) : (
          <Card>
            <div className="px-5 py-4 text-xs font-semibold text-t2 border-b border-border-dim">
              {windows.length} risk window{windows.length !== 1 ? 's' : ''} in next {duration} min
            </div>
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border-dim">
                  {['Start', 'End', 'Satellite', 'Az (°)', 'El (°)'].map((h) => (
                    <th key={h} className="px-4 py-2.5 text-left font-medium text-t3 uppercase tracking-wider">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {windows.map((w, i) => (
                  <tr key={i} className="border-b border-border-dim last:border-0 hover:bg-orbit transition-colors">
                    <td className="px-4 py-3 font-mono text-t1">{new Date(w.start).toLocaleTimeString()}</td>
                    <td className="px-4 py-3 font-mono text-t1">{new Date(w.end).toLocaleTimeString()}</td>
                    <td className="px-4 py-3 text-t2">{w.sat_id}</td>
                    <td className="px-4 py-3 font-mono text-t1">{w.azimuth.toFixed(1)}</td>
                    <td className="px-4 py-3 font-mono text-t1">{w.elevation.toFixed(1)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        )
      )}

      {!ran && !loading && (
        <Card className="p-8 text-center text-sm text-t3">
          Set a duration above and click <span className="text-blue">Run prediction</span> to forecast risk windows.<br />
          <span className="text-[11px] mt-1 block">Requires observer location and sufficient daemon telemetry.</span>
        </Card>
      )}
    </div>
  )
}
