import { NavLink, useLocation } from 'react-router-dom'
import clsx from 'clsx'

const nav = [
  {
    section: 'Monitor',
    items: [
      { to: '/',            label: 'Status',      icon: <StatusIcon /> },
      { to: '/insights',    label: 'Insights',    icon: <InsightsIcon /> },
      { to: '/obstruction', label: 'Obstruction', icon: <CompassIcon /> },
    ],
  },
  {
    section: 'Analyze',
    items: [
      { to: '/report',  label: 'Report',  icon: <ReportIcon /> },
      { to: '/predict', label: 'Predict', icon: <PredictIcon /> },
    ],
  },
]

export function NavSidebar({ hostname }: { hostname?: string }) {
  return (
    <nav className="w-[220px] flex-shrink-0 bg-space border-r border-border-dim flex flex-col py-5 px-3">
      {/* Logo */}
      <div className="flex items-center gap-3 px-3 mb-6">
        <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-blue to-[#0044aa] flex items-center justify-center flex-shrink-0">
          <GlobeIcon />
        </div>
        <div>
          <div className="font-semibold text-sm tracking-tight text-t1">Ground Control</div>
          <div className="font-mono text-[10px] text-t3 mt-0.5">{hostname ?? 'starlink-pi'}</div>
        </div>
      </div>

      {nav.map((group) => (
        <div key={group.section} className="mb-2">
          <div className="px-3 mb-1 text-[10px] font-semibold uppercase tracking-widest text-t3">
            {group.section}
          </div>
          {group.items.map((item) => (
            <NavItem key={item.to} to={item.to} icon={item.icon} label={item.label} />
          ))}
          <div className="h-px bg-border-dim my-3" />
        </div>
      ))}
    </nav>
  )
}

function NavItem({ to, icon, label }: { to: string; icon: React.ReactNode; label: string }) {
  return (
    <NavLink
      to={to}
      end={to === '/'}
      className={({ isActive }) =>
        clsx(
          'flex items-center gap-2.5 px-3 py-2 rounded-md text-[13px] font-medium mb-0.5 transition-colors',
          isActive
            ? 'bg-blue/10 text-blue border border-blue/15'
            : 'text-t2 hover:bg-orbit hover:text-t1 border border-transparent',
        )
      }
    >
      {icon}
      {label}
    </NavLink>
  )
}

// ── Icons (inline SVG, 15×15) ──────────────────────────────────────────────

function GlobeIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2.5">
      <circle cx="12" cy="12" r="10" />
      <path d="M2 12h20M12 2a15 15 0 0 1 4 10 15 15 0 0 1-4 10 15 15 0 0 1-4-10 15 15 0 0 1 4-10z" />
    </svg>
  )
}

function StatusIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="10" />
      <line x1="12" y1="8" x2="12" y2="12" />
      <line x1="12" y1="16" x2="12.01" y2="16" />
    </svg>
  )
}

function InsightsIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
    </svg>
  )
}

function CompassIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="10" />
      <path d="M2 12h20M12 2a15 15 0 0 1 4 10 15 15 0 0 1-4 10" />
    </svg>
  )
}

function ReportIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <rect x="3" y="3" width="7" height="7" /><rect x="14" y="3" width="7" height="7" />
      <rect x="14" y="14" width="7" height="7" /><rect x="3" y="14" width="7" height="7" />
    </svg>
  )
}

function PredictIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" />
    </svg>
  )
}
