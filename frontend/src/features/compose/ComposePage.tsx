import { useMemo, useState } from 'react'
import {
  CaretRightOutlined, EditOutlined, FileTextOutlined, LinkOutlined, MoreOutlined, PauseOutlined,
  PlayCircleOutlined, ReloadOutlined, SearchOutlined, SyncOutlined, UploadOutlined,
} from '@ant-design/icons'
import { Alert, Button, Dropdown, Input, Popconfirm, Select, Space, Table, Tooltip, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { StateBadge } from '../../components/StateBadge'
import { useResource } from '../../hooks/useResource'
import type { Container, Project } from '../../types'
import { formatBytes, formatDate, formatPercent } from '../../utils/format'
import { ComposeEditorDrawer } from './ComposeEditorDrawer'
import { LogsDrawer } from './LogsDrawer'

export function ComposePage() {
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('all')
  const [editorProject, setEditorProject] = useState<Project>()
  const [logsProject, setLogsProject] = useState<Project>()
  const [busyKey, setBusyKey] = useState('')
  const { data = [], error, loading, refresh } = useResource(api.projects, [])
  const [messageApi, contextHolder] = message.useMessage()

  const filtered = useMemo(() => data.filter((project) => {
    const matchesText = project.name.toLowerCase().includes(query.trim().toLowerCase())
    const matchesStatus = status === 'all' || project.status === status || (status === 'updates' && project.updateStatus === 'available')
    return matchesText && matchesStatus
  }), [data, query, status])

  const runAction = async (project: Project, action: string) => {
    setBusyKey(`${project.key}:${action}`)
    try {
      await api.projectAction(project.key, action)
      messageApi.success(`${project.name} 操作已完成`)
      await refresh()
    } catch (reason) {
      messageApi.error(reason instanceof Error ? reason.message : '操作失败')
    } finally {
      setBusyKey('')
    }
  }

  const startUpdate = async (project: Project) => {
    try {
      await api.runUpdate(project.key)
      messageApi.success(`${project.name} 已开始更新，请在左侧“更新进度”查看`)
    } catch (reason) {
      messageApi.error(reason instanceof Error ? reason.message : '无法开始更新')
    }
  }

  const columns: ColumnsType<Project> = [
    { title: '项目名称', dataIndex: 'name', width: 145, sorter: (a, b) => a.name.localeCompare(b.name), render: (name, project) => <div className="project-name"><span className="project-icon"><CaretRightOutlined /></span><div><strong>{name}</strong><small>{project.discoverySource}</small></div></div> },
    { title: '运行状态', dataIndex: 'status', width: 76, align: 'center', sorter: (a, b) => Number(a.status === 'running') - Number(b.status === 'running'), defaultSortOrder: 'ascend', sortDirections: ['ascend', 'descend', 'ascend'], render: (value) => <StateBadge state={value} /> },
    { title: 'CPU', dataIndex: 'cpuPercent', width: 62, sorter: (a, b) => a.cpuPercent - b.cpuPercent, render: formatPercent },
    { title: '内存', dataIndex: 'memoryBytes', width: 76, sorter: (a, b) => a.memoryBytes - b.memoryBytes, render: formatBytes },
    {
      title: '操作', key: 'actions', fixed: 'right', width: 250,
      render: (_, project) => (
        <div className="row-actions">
          <Tooltip title="启动"><Button aria-label={`启动 ${project.name}`} type="text" size="small" icon={<PlayCircleOutlined />} disabled={project.status === 'running'} loading={busyKey === `${project.key}:start`} onClick={() => void runAction(project, 'start')} /></Tooltip>
          <Popconfirm title={`停止 ${project.name}？`} description={`这将停止该项目的 ${project.total} 个容器。`} okText="停止" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => void runAction(project, 'stop')}>
            <Button aria-label={`停止 ${project.name}`} danger type="text" size="small" icon={<PauseOutlined />} disabled={project.status === 'stopped'} loading={busyKey === `${project.key}:stop`} />
          </Popconfirm>
          <Tooltip title="重启"><Button aria-label={`重启 ${project.name}`} type="text" size="small" icon={<SyncOutlined />} loading={busyKey === `${project.key}:restart`} onClick={() => void runAction(project, 'restart')} /></Tooltip>
          <Popconfirm title={`更新 ${project.name}？`} description="将拉取镜像、重新创建容器并记录结果。" okText="更新" cancelText="取消" onConfirm={() => void startUpdate(project)}>
            <Tooltip title="更新"><Button aria-label={`更新 ${project.name}`} type="text" size="small" icon={<UploadOutlined />} /></Tooltip>
          </Popconfirm>
          <Tooltip title="编辑"><Button aria-label={`编辑 ${project.name}`} type="text" size="small" icon={<EditOutlined />} disabled={!project.editable} onClick={() => setEditorProject(project)} /></Tooltip>
          <Tooltip title="访问"><Button aria-label={`访问 ${project.name}`} type="text" size="small" icon={<LinkOutlined />} disabled={!project.internalUrl} href={project.internalUrl} target="_blank" /></Tooltip>
          <Tooltip title="日志"><Button aria-label={`日志 ${project.name}`} type="text" size="small" icon={<FileTextOutlined />} onClick={() => setLogsProject(project)} /></Tooltip>
          <Dropdown trigger={['click']} menu={{ items: [{ key: 'path', label: project.configFile || '未定位 Compose 文件', disabled: true }, { key: 'refresh', label: '重新发现', icon: <ReloadOutlined />, onClick: () => void refresh() }] }}>
            <Button aria-label={`更多 ${project.name}`} type="text" size="small" icon={<MoreOutlined />} />
          </Dropdown>
        </div>
      ),
    },
  ]

  return (
    <section className="page compose-page">
      {contextHolder}
      <PageHeader title="Compose 项目" description="每 24 小时自动检查镜像更新；发现新版后由你确认是否更新。" />
      <div className="table-toolbar">
        <Space size={10}>
          <Input allowClear className="search-input" prefix={<SearchOutlined />} placeholder="搜索项目名称、路径或描述…" value={query} onChange={(event) => setQuery(event.target.value)} />
          <Select value={status} onChange={setStatus} options={[{ value: 'all', label: '全部状态' }, { value: 'running', label: '运行中' }, { value: 'stopped', label: '已停止' }, { value: 'degraded', label: '异常' }, { value: 'updates', label: '有更新' }]} />
        </Space>
        <Button icon={<ReloadOutlined />} onClick={() => void refresh()} loading={loading}>刷新</Button>
      </div>
      {error ? <Alert className="inline-alert" type="warning" showIcon title="无法读取 Docker 数据" description={error.message} action={<Button size="small" onClick={() => void refresh()}>重试</Button>} /> : null}
      <Table<Project>
        className="dense-table project-table"
        rowKey="key"
        size="small"
        columns={columns}
        dataSource={filtered}
        loading={loading}
        scroll={{ x: 650 }}
        pagination={{ pageSize: 20, showSizeChanger: true, pageSizeOptions: [15, 20, 25, 50], showTotal: (total) => `共 ${total} 项` }}
        expandable={{ expandedRowRender: (project) => <ContainerSubtable containers={project.containers} />, rowExpandable: (project) => project.containers.length > 0, columnWidth: 32 }}
      />
      <ComposeEditorDrawer project={editorProject} open={Boolean(editorProject)} onClose={() => setEditorProject(undefined)} onSaved={refresh} />
      <LogsDrawer project={logsProject} open={Boolean(logsProject)} onClose={() => setLogsProject(undefined)} />
    </section>
  )
}

function ContainerSubtable({ containers }: { containers: Container[] }) {
  const columns: ColumnsType<Container> = [
    { title: '容器名称', dataIndex: 'name', width: 180, render: (value) => <strong>{value}</strong> },
    { title: '状态', dataIndex: 'state', width: 68, align: 'center', render: (value) => <StateBadge state={value} /> },
    { title: '镜像', dataIndex: 'image', ellipsis: true },
    { title: 'CPU', dataIndex: 'cpuPercent', width: 90, render: formatPercent },
    { title: '内存', dataIndex: 'memoryBytes', width: 100, render: formatBytes },
    { title: '启动时间', dataIndex: 'createdAt', width: 120, render: formatDate },
    { title: '操作', width: 90, render: () => <Button size="small" type="link" icon={<FileTextOutlined />}>日志</Button> },
  ]
  return <Table<Container> className="subtable" rowKey="id" size="small" columns={columns} dataSource={containers} pagination={false} />
}
