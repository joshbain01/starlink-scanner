import type {
  StatusResponse,
  InsightEvent,
  SpatialBucket,
  ReportResponse,
  RiskWindow,
} from '@/types'

const BASE = '/api'

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json() as Promise<T>
}

export const api = {
  status:  (): Promise<StatusResponse>   => get('/status'),
  insights: (params?: { loss_threshold?: number; snr_delta?: number; noise_delta?: number }): Promise<InsightEvent[]> => {
    const q = new URLSearchParams()
    if (params?.loss_threshold != null) q.set('loss_threshold', String(params.loss_threshold))
    if (params?.snr_delta      != null) q.set('snr_delta',      String(params.snr_delta))
    if (params?.noise_delta    != null) q.set('noise_delta',    String(params.noise_delta))
    const qs = q.toString()
    return get(`/insights${qs ? '?' + qs : ''}`)
  },
  buckets:  (lossThreshold = 0.05): Promise<SpatialBucket[]> => get(`/buckets?loss_threshold=${lossThreshold}`),
  report:   (): Promise<ReportResponse>  => get('/report'),
  predict:  (durationMins: number): Promise<RiskWindow[]> => get(`/predict?duration=${durationMins}`),
}
