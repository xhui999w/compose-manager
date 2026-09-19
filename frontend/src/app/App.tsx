import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { App as AntApp, ConfigProvider, Skeleton, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { AppShell, type PageKey } from './AppShell'
import { ComposePage } from '../features/compose/ComposePage'

const ContainersPage = lazy(() => import('../features/containers/ContainersPage').then((module) => ({ default: module.ContainersPage })))
const ImagesPage = lazy(() => import('../features/images/ImagesPage').then((module) => ({ default: module.ImagesPage })))
const UpdatesPage = lazy(() => import('../features/updates/UpdatesPage').then((module) => ({ default: module.UpdatesPage })))
const SettingsPage = lazy(() => import('../features/settings/SettingsPage').then((module) => ({ default: module.SettingsPage })))

export function App() {
  const [page, setPage] = useState<PageKey>('compose')
  const [dark, setDark] = useState(() => localStorage.getItem('cm-theme') === 'dark')
  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
    document.documentElement.dataset.density = localStorage.getItem('cm-density') ?? 'compact'
  }, [dark])
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
        <AppShell page={page} onPageChange={setPage}><Suspense fallback={<div className="page"><Skeleton active /></div>}>{content}</Suspense></AppShell>
      </AntApp>
    </ConfigProvider>
  )
}
