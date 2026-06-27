import clsx from 'clsx'
import type { SignalLevel } from '@/types'

// ── StatCard ───────────────────────────────────────────────────────────────

interface StatCardProps {
  label: string
  value: string
  sub?: string
  level?: SignalLevel
}

const accentMap: Record<SignalLevel, string> = {
  healthy:  'before:bg-green',
  degraded: 'before:bg-amber',
  critical: 'before:bg-danger',
  unknown:  'before:bg-t3',
}

const valueMap: Record<SignalLevel, string> = {
  healthy:  'text-green',
  degraded: 'text-amber',
  critical: 'text-danger',
  unknown:  'text-t1',
}

export function StatCard({ label, value, sub, level = 'unknown' }: StatCardProps) {
  return (
    <div
      className={clsx(
        'relative bg-space border border-border-dim rounded-lg px-5 py-4 overflow-hidden',
        'before:content-[""] before:absolute before:top-0 before:left-0 before:right-0 before:h-0.5',
        accentMap[level],
      )}
    >
      <div className="text-[11px] font-medium uppercase tracking-widest text-t3 mb-2">{label}</div>
      <div className={clsx('font-mono text-[28px] font-medium leading-none', valueMap[level])}>{value}</div>
      {sub && <div className="text-xs text-t2 mt-1.5">{sub}</div>}
    </div>
  )
}

// ── AlertBadge ────────────────────────────────────────────────────────────

interface BadgeProps {
  color?: 'green' | 'blue' | 'amber' | 'red' | 'gray'
  children: React.ReactNode
}

const badgeStyles = {
  green: 'bg-green/10 text-green border border-green/20',
  blue:  'bg-blue/10  text-blue  border border-blue/20',
  amber: 'bg-amber/10 text-amber border border-amber/20',
  red:   'bg-danger/10 text-danger border border-danger/20',
  gray:  'bg-t3/10 text-t2 border border-t3/20',
}

export function Badge({ color = 'gray', children }: BadgeProps) {
  return (
    <span className={clsx('inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium', badgeStyles[color])}>
      <span className={clsx('w-1.5 h-1.5 rounded-full flex-shrink-0', {
        'bg-green':  color === 'green',
        'bg-blue':   color === 'blue',
        'bg-amber':  color === 'amber',
        'bg-danger': color === 'red',
        'bg-t3':     color === 'gray',
      })} />
      {children}
    </span>
  )
}

// ── CauseBadge ────────────────────────────────────────────────────────────

import type { DropCause } from '@/types'

const causeStyles: Record<DropCause, string> = {
  'rf-blockage':  'bg-danger/10 text-[#ff7a8e] border border-danger/20',
  'emi':          'bg-amber/10  text-[#ffc933] border border-amber/20',
  'dish-signal':  'bg-blue/10   text-[#4dc8ff] border border-blue/20',
  'congestion':   'bg-t3/10     text-t2        border border-t3/20',
  'unknown':      'bg-t3/10     text-t3        border border-t3/20',
}

const causeLabels: Record<DropCause, string> = {
  'rf-blockage': '[RF] Blockage / Handoff',
  'emi':         '[RF] EMI / Radar',
  'dish-signal': '[dish] Signal Alert',
  'congestion':  '[!] Congestion',
  'unknown':     'Unknown',
}

export function CauseBadge({ cause }: { cause: DropCause }) {
  return (
    <span className={clsx('text-[11px] font-medium px-2 py-0.5 rounded', causeStyles[cause])}>
      {causeLabels[cause]}
    </span>
  )
}

// ── LiveIndicator ──────────────────────────────────────────────────────────

export function LiveIndicator({ interval = '15s' }: { interval?: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-xs font-medium text-green">
      <span className="live-dot w-2 h-2 rounded-full bg-green flex-shrink-0" />
      Live · {interval}
    </span>
  )
}

// ── SignalBars ─────────────────────────────────────────────────────────────

export function SignalBars({ level }: { level: SignalLevel }) {
  const color = level === 'healthy' ? 'bg-green' : level === 'degraded' ? 'bg-amber' : 'bg-danger'
  const lit = level === 'healthy' ? 5 : level === 'degraded' ? 3 : 1
  const bars = [8, 11, 14, 17, 20]
  return (
    <div className="flex items-end gap-0.5" style={{ height: 20 }}>
      {bars.map((h, i) => (
        <div
          key={i}
          className={clsx('w-1.5 rounded-sm', i < lit ? color : 'bg-atmos')}
          style={{ height: h }}
        />
      ))}
    </div>
  )
}

// ── PageHeader ────────────────────────────────────────────────────────────

interface PageHeaderProps {
  title: string
  sub?: string
  live?: boolean
  actions?: React.ReactNode
}

export function PageHeader({ title, sub, live, actions }: PageHeaderProps) {
  return (
    <div className="flex items-center justify-between mb-6">
      <div>
        <h1 className="text-lg font-bold tracking-tight">{title}</h1>
        {sub && <div className="font-mono text-xs text-t3 mt-0.5">{sub}</div>}
      </div>
      <div className="flex items-center gap-3">
        {live && <LiveIndicator />}
        {actions}
      </div>
    </div>
  )
}

// ── Spinner ───────────────────────────────────────────────────────────────

export function Spinner() {
  return (
    <div className="flex items-center justify-center py-16">
      <svg className="animate-spin w-6 h-6 text-blue" viewBox="0 0 24 24" fill="none">
        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
      </svg>
    </div>
  )
}

// ── ErrorMessage ──────────────────────────────────────────────────────────

export function ErrorMessage({ message }: { message: string }) {
  return (
    <div className="rounded-lg border border-danger/20 bg-danger/5 px-4 py-3 text-sm text-danger">
      {message}
    </div>
  )
}

// ── Card ──────────────────────────────────────────────────────────────────

export function Card({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={clsx('bg-space border border-border-dim rounded-lg', className)}>
      {children}
    </div>
  )
}
