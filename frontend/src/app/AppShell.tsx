import type { ReactNode } from 'react'
import { ClusterOutlined, ContainerOutlined, DatabaseOutlined, FileTextOutlined, SettingOutlined } from '@ant-design/icons'
import { Layout, Menu } from 'antd'
import type { MenuProps } from 'antd'
import { SystemStatusBar } from './SystemStatusBar'

export type PageKey = 'compose' | 'containers' | 'images' | 'updates' | 'settings'

const items: MenuProps['items'] = [
  { key: 'compose', icon: <ClusterOutlined />, label: 'Compose' },
  { key: 'containers', icon: <ContainerOutlined />, label: '容器' },
  { key: 'images', icon: <DatabaseOutlined />, label: '镜像' },
  { key: 'updates', icon: <FileTextOutlined />, label: '更新记录' },
  { key: 'settings', icon: <SettingOutlined />, label: '设置' },
]

export function AppShell({ page, onPageChange, children }: { page: PageKey; onPageChange: (page: PageKey) => void; children: ReactNode }) {
  return (
    <Layout className="app-shell">
      <Layout.Sider className="app-sidebar" width={196} theme="light" breakpoint="xl" collapsedWidth={60}>
        <div className="brand">
          <div className="brand-mark"><ClusterOutlined /></div>
          <div className="brand-copy"><strong>Compose Manager</strong><span>更简单的 Compose 管理</span></div>
        </div>
        <Menu className="app-menu" mode="inline" selectedKeys={[page]} items={items} onClick={({ key }) => onPageChange(key as PageKey)} />
        <div className="sidebar-foot"><span className="health-dot" /><span>Docker Engine</span></div>
      </Layout.Sider>
      <Layout className="app-main">
        <SystemStatusBar />
        <Layout.Content className="app-content">{children}</Layout.Content>
      </Layout>
    </Layout>
  )
}
