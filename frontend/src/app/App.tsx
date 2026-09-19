import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import { App as AntApp, ConfigProvider, Skeleton, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { api, setUnauthorizedHandler } from '../api/client'
import { AppShell, type PageKey } from './AppShell'
import { LoginPage } from './LoginPage'
import { ComposePage } from '../features/compose/ComposePage'

const ContainersPage = lazy(() => import('../features/containers/ContainersPage').then((module) => ({ default: module.ContainersPage })))
const ImagesPage = lazy(() => import('../features/images/ImagesPage').then((module) => ({ default: module.ImagesPage })))
const UpdatesPage = lazy(() => import('../features/updates/UpdatesPage').then((module) => ({ default: module.UpdatesPage })))
const SettingsPage = lazy(() => import('../features/settings/SettingsPage').then((module) => ({ default: module.SettingsPage })))

// checking：启动时探测会话；anonymous：需要登录或会话已失效；authenticated：可进入面板。
type AuthState = 'checking' | 'anonymous' | 'authenticated'

export function App() {
  const [page, setPage] = useState<PageKey>('compose')
  const [dark, setDark] = useState(() => localStorage.getItem('cm-theme') === 'dark')
  const [authState, setAuthState] = useState<AuthState>('checking')
  const [username, setUsername] = useState('')

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
    document.documentElement.dataset.density = localStorage.getItem('cm-density') ?? 'compact'
  }, [dark])

  useEffect(() => {
    let active = true
    api.session()
      .then((session) => {
        if (!active) return
        setUsername(session.username)
        // required=false 表示服务端未配置口令，此时直接放行，不要求登录。
        setAuthState(session.required && !session.authenticated ? 'anonymous' : 'authenticated')
      })
      .catch(() => {
        // 探测失败通常是后端异常：退回登录页，让登录请求把真实错误暴露给用户。
        if (active) setAuthState('anonymous')
      })
    return () => { active = false }
  }, [])

  // 会话在别处失效（改了口令、换了签名密钥、Cookie 过期）时，任意接口的 401 都会退到登录页。
  useEffect(() => {
    setUnauthorizedHandler(() => { setAuthState('anonymous') })
    return () => setUnauthorizedHandler(null)
  }, [])

  const handleLoginSuccess = useCallback((name: string) => {
    setUsername(name)
    setAuthState('authenticated')
  }, [])

  const handleLogout = useCallback(() => {
    void api.logout().catch(() => undefined).finally(() => setAuthState('anonymous'))
  }, [])

  const content = useMemo(() => {
    switch (page) {
      case 'containers': return <ContainersPage />
      case 'images': return <ImagesPage />
      case 'updates': return <UpdatesPage />
      case 'settings': return <SettingsPage dark={dark} onThemeChange={setDark} />
      default: return <ComposePage />
    }
  }, [page, dark])

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: '#2f6fed', colorSuccess: '#20a464', colorWarning: '#e89b13', colorError: '#dc3948',
          borderRadius: 5, fontSize: 13, controlHeight: 32, colorBgLayout: dark ? '#111827' : '#f6f8fb',
        },
        components: {
          Table: { cellPaddingBlockSM: 7, cellPaddingInlineSM: 10, headerBg: dark ? '#1f2937' : '#f7f9fc', headerColor: dark ? '#d8deea' : '#516079' },
          Menu: { itemHeight: 38, itemBorderRadius: 5 },
          Button: { controlHeightSM: 26, paddingInlineSM: 7 },
        },
      }}
    >
      <AntApp>
        {authState === 'checking' ? (
          <div className="boot-screen"><Skeleton active paragraph={{ rows: 3 }} style={{ width: 320 }} /></div>
        ) : authState === 'anonymous' ? (
          <LoginPage onSuccess={handleLoginSuccess} />
        ) : (
          <AppShell page={page} onPageChange={setPage} username={username} onLogout={handleLogout}>
            <Suspense fallback={<div className="page"><Skeleton active /></div>}>{content}</Suspense>
          </AppShell>
        )}
      </AntApp>
    </ConfigProvider>
  )
}
