import { NavSidebar } from './NavSidebar'

interface Props {
  hostname?: string
  children: React.ReactNode
}

export function AppShell({ hostname, children }: Props) {
  return (
    <div className="flex h-screen w-screen overflow-hidden bg-void text-t1 font-ui">
      <NavSidebar hostname={hostname} />
      <main className="flex-1 overflow-y-auto">
        {children}
      </main>
    </div>
  )
}
