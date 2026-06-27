import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { AppShell } from '@/components/layout/AppShell'
import { StatusView }      from '@/views/StatusView'
import { InsightsView }    from '@/views/InsightsView'
import { ObstructionView } from '@/views/ObstructionView'
import { ReportView }      from '@/views/ReportView'
import { PredictView }     from '@/views/PredictView'

export default function App() {
  return (
    <BrowserRouter>
      <AppShell hostname={import.meta.env.VITE_HOSTNAME ?? 'openclaw-pi'}>
        <Routes>
          <Route path="/"            element={<StatusView />} />
          <Route path="/insights"    element={<InsightsView />} />
          <Route path="/obstruction" element={<ObstructionView />} />
          <Route path="/report"      element={<ReportView />} />
          <Route path="/predict"     element={<PredictView />} />
        </Routes>
      </AppShell>
    </BrowserRouter>
  )
}
