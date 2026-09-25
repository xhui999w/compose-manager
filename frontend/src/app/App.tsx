import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { App as AntApp, ConfigProvider, Skeleton, Spin, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { AUTH_REQUIRED_EVENT, api } from '../api/client'
import type { AuthStatus } from '../types'
import { AuthPage } from '../features/auth/AuthPage'
import { ComposePage } from '../features/compose/ComposePage'
import { AppShell, type PageKey } from './AppShell'

const WorkspacePage = lazy(() => import('../features/workspace/WorkspacePage').then((module) => ({ default: module.WorkspacePage })))
const ImagesPage = lazy(() => import('../features/images/ImagesPage').then((module) => ({ default: module.ImagesPage })))
const UpdateProgressPage = lazy(() => import('../features/updates/UpdateProgressPage').then((module) => ({ default: module.UpdateProgressPage })))
const UpdatesPage = lazy(() => import('../features/updates/UpdatesPage').then((module) => ({ default: module.UpdatesPage })))
const SettingsPage = lazy(() => import('../features/settings/SettingsPage').then((module) => ({ default: module.SettingsPage })))

let initialAuthPromise: Promise<AuthStatus> | undefined

function loadInitialAuth() {
  initialAuthPromise ??= api.authStatus()
  return initialAuthPromise
}

function AuthGate({ dark, onThemeChange }: { dark: boolean; onThemeChange: (dark: boolean) => void }) {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [loadError, setLoadError] = useState('')

  useEffect(() => {
    let active = true
    void loadInitialAuth().then((value) => { if (active) setStatus(value) }).catch((reason) => {
      if (active) setLoadError(reason instanceof Error ? reason.message : '无法连接服务')
    })
    const requireAuth = () => setStatus((current) => ({ setupRequired: current?.setupRequired ?? false, authenticated: false }))
    window.addEventListener(AUTH_REQUIRED_EVENT, requireAuth)
    return () => { active = false; window.removeEventListener(AUTH_REQUIRED_EVENT, requireAuth) }
  }, [])

  if (loadError) return <main className="auth-page"><section className="auth-panel"><h1>无法连接 Compose Manager</h1><p>{loadError}</p><button type="button" onClick={() => window.location.reload()}>重新加载</button></section></main>
  if (!status) return <main className="auth-page auth-page--loading"><Spin size="large" description="正在检查登录状态…"><div className="auth-loading-space" /></Spin></main>
  if (!status.authenticated) return <AuthPage setupRequired={status.setupRequired} onAuthenticated={setStatus} />

  const logout = async () => {
    try { await api.logout() } finally { setStatus({ setupRequired: false, authenticated: false }) }
  }
  return <AuthenticatedApp dark={dark} onThemeChange={onThemeChange} username={status.username ?? 'admin'} onLogout={() => void logout()} />
}

function AuthenticatedApp({ dark, onThemeChange, username, onLogout }: { dark: boolean; onThemeChange: (dark: boolean) => void; username: string; onLogout: () => void }) {
  const [page, setPage] = useState<PageKey>('compose')
  const content = useMemo(() => {
    switch (page) {
      case 'workspace': return <WorkspacePage />
      case 'images': return <ImagesPage />
      case 'progress': return <UpdateProgressPage />
      case 'updates': return <UpdatesPage />
      case 'settings': return <SettingsPage dark={dark} onThemeChange={onThemeChange} />
      default: return <ComposePage />
    }
  }, [page, dark, onThemeChange])

  return <AppShell page={page} onPageChange={setPage} username={username} onLogout={onLogout}><Suspense fallback={<div className="page"><Skeleton active /></div>}>{content}</Suspense></AppShell>
}

export function App() {
  const [dark, setDark] = useState(() => localStorage.getItem('cm-theme') === 'dark')
  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
    document.documentElement.dataset.density = localStorage.getItem('cm-density') ?? 'compact'
  }, [dark])

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: { colorPrimary: '#2f6fed', colorSuccess: '#20a464', colorWarning: '#e89b13', colorError: '#dc3948', borderRadius: 5, fontSize: 13, controlHeight: 32, colorBgLayout: dark ? '#111827' : '#f6f8fb' },
        components: {
          Table: { cellPaddingBlockSM: 7, cellPaddingInlineSM: 10, headerBg: dark ? '#1f2937' : '#f7f9fc', headerColor: dark ? '#d8deea' : '#516079' },
          Menu: { itemHeight: 38, itemBorderRadius: 5 },
          Button: { controlHeightSM: 26, paddingInlineSM: 7 },
        },
      }}
    >
      <AntApp><AuthGate dark={dark} onThemeChange={setDark} /></AntApp>
    </ConfigProvider>
  )
}
