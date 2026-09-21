import type { ReactNode } from 'react'
import { CloudDownloadOutlined, ClusterOutlined, ContainerOutlined, DatabaseOutlined, FileTextOutlined, LogoutOutlined, SettingOutlined, UserOutlined } from '@ant-design/icons'
import { Button, Layout, Menu, Tooltip } from 'antd'
import type { MenuProps } from 'antd'
import { SystemStatusBar } from './SystemStatusBar'

export type PageKey = 'compose' | 'containers' | 'images' | 'progress' | 'updates' | 'settings'

const items: MenuProps['items'] = [
  { key: 'compose', icon: <ClusterOutlined />, label: 'Compose' },
  { key: 'containers', icon: <ContainerOutlined />, label: '容器' },
  { key: 'images', icon: <DatabaseOutlined />, label: '镜像' },
  { key: 'progress', icon: <CloudDownloadOutlined />, label: '更新进度' },
  { key: 'updates', icon: <FileTextOutlined />, label: '更新记录' },
  { key: 'settings', icon: <SettingOutlined />, label: '设置' },
]

export function AppShell({ page, onPageChange, username, onLogout, children }: { page: PageKey; onPageChange: (page: PageKey) => void; username: string; onLogout: () => void; children: ReactNode }) {
  return (
    <Layout className="app-shell">
      <Layout.Sider className="app-sidebar" width={196} theme="light" breakpoint="xl" collapsedWidth={60}>
        <div className="brand">
          <div className="brand-mark"><ClusterOutlined /></div>
          <div className="brand-copy"><strong>Compose Manager</strong><span>更简单的 Compose 管理</span></div>
        </div>
        <Menu className="app-menu" mode="inline" selectedKeys={[page]} items={items} onClick={({ key }) => onPageChange(key as PageKey)} />
        <div className="sidebar-foot">
          <div className="sidebar-engine"><span className="health-dot" /><span>Docker Engine</span></div>
          <div className="sidebar-user"><UserOutlined /><span title={username}>{username}</span><Tooltip title="退出登录"><Button aria-label="退出登录" type="text" size="small" icon={<LogoutOutlined />} onClick={onLogout} /></Tooltip></div>
        </div>
      </Layout.Sider>
      <Layout className="app-main">
        <SystemStatusBar />
        <Layout.Content className="app-content">{children}</Layout.Content>
      </Layout>
    </Layout>
  )
}
