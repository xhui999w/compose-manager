import { useMemo, useState } from 'react'
import {
  CaretRightOutlined, EditOutlined, FileTextOutlined, LinkOutlined, MoreOutlined, PauseOutlined,
  PlayCircleOutlined, ReloadOutlined, SearchOutlined, SyncOutlined, UploadOutlined,
} from '@ant-design/icons'
import { Alert, Button, Dropdown, Empty, Input, Pagination, Popconfirm, Segmented, Select, Space, Spin, Table, Tooltip, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { StateBadge } from '../../components/StateBadge'
import { useResource } from '../../hooks/useResource'
import type { Container, Project } from '../../types'
import { formatBytes, formatDate, formatPercent } from '../../utils/format'
import { ComposeEditorDrawer } from './ComposeEditorDrawer'
import { LogsDrawer } from './LogsDrawer'

const PROJECT_PAGE_SIZE = 30

type ProjectSort = 'issues' | 'running' | 'memory' | 'cpu' | 'name'

const PROJECT_SORT_OPTIONS = [
  { value: 'issues', label: '异常 / 停止' },
  { value: 'running', label: '运行中' },
  { value: 'memory', label: '内存 ↓' },
  { value: 'cpu', label: 'CPU ↓' },
  { value: 'name', label: '名称' },
]

function sortProjects(projects: Project[], sort: ProjectSort) {
  return [...projects].sort((a, b) => {
    let result: number
    if (sort === 'issues' || sort === 'running') {
      result = Number(a.status === 'running') - Number(b.status === 'running')
      if (sort === 'running') result *= -1
    } else if (sort === 'memory') result = b.memoryBytes - a.memoryBytes
    else if (sort === 'cpu') result = b.cpuPercent - a.cpuPercent
    else result = a.name.localeCompare(b.name)
    return result || a.name.localeCompare(b.name)
  })
}

type ProjectCompactRowProps = {
  project: Project
  busyKey: string
  onAction: (project: Project, action: string) => Promise<void>
  onUpdate: (project: Project) => Promise<void>
  onEdit: (project: Project) => void
  onLogs: (project: Project) => void
  onRefresh: () => Promise<void>
}

function ProjectCompactRow({ project, busyKey, onAction, onUpdate, onEdit, onLogs, onRefresh }: ProjectCompactRowProps) {
  const [expanded, setExpanded] = useState(false)
  const running = project.status === 'running'
  return (
    <article className={`project-compact-row${expanded ? ' is-expanded' : ''}`}>
      <div className="project-compact-row__main">
        <Button
          className="project-expand"
          type="text"
          size="small"
          icon={<CaretRightOutlined />}
          aria-label={`${expanded ? '收起' : '展开'} ${project.name}`}
          aria-expanded={expanded}
          disabled={project.containers.length === 0}
          onClick={() => setExpanded((value) => !value)}
        />
        <div className="project-name">
          <span className="project-icon"><CaretRightOutlined /></span>
          <div><Tooltip title={project.name}><strong>{project.name}</strong></Tooltip><small>{project.discoverySource}</small></div>
        </div>
        <StateBadge state={project.status} />
        <span className="project-metric"><small>CPU</small>{formatPercent(project.cpuPercent)}</span>
        <span className="project-metric"><small>内存</small>{formatBytes(project.memoryBytes)}</span>
        <div className="row-actions">
          {running ? (
            <Popconfirm title={`停止 ${project.name}？`} description={`这将停止该项目的 ${project.total} 个容器。`} okText="停止" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => void onAction(project, 'stop')}>
              <Tooltip title="停止"><Button aria-label={`停止 ${project.name}`} danger type="text" size="small" icon={<PauseOutlined />} loading={busyKey === `${project.key}:stop`} /></Tooltip>
            </Popconfirm>
          ) : (
            <Tooltip title="启动"><Button aria-label={`启动 ${project.name}`} type="text" size="small" icon={<PlayCircleOutlined />} loading={busyKey === `${project.key}:start`} onClick={() => void onAction(project, 'start')} /></Tooltip>
          )}
          <Tooltip title="重启"><Button aria-label={`重启 ${project.name}`} type="text" size="small" icon={<SyncOutlined />} loading={busyKey === `${project.key}:restart`} onClick={() => void onAction(project, 'restart')} /></Tooltip>
          <Popconfirm title={`更新 ${project.name}？`} description="将拉取镜像、重新创建容器并记录结果。" okText="更新" cancelText="取消" onConfirm={() => void onUpdate(project)}>
            <Tooltip title="更新"><Button aria-label={`更新 ${project.name}`} type="text" size="small" icon={<UploadOutlined />} /></Tooltip>
          </Popconfirm>
          <Tooltip title="编辑"><Button aria-label={`编辑 ${project.name}`} type="text" size="small" icon={<EditOutlined />} disabled={!project.editable} onClick={() => onEdit(project)} /></Tooltip>
          <Tooltip title="访问"><Button aria-label={`访问 ${project.name}`} type="text" size="small" icon={<LinkOutlined />} disabled={!project.internalUrl} href={project.internalUrl} target="_blank" /></Tooltip>
          <Tooltip title="日志"><Button aria-label={`日志 ${project.name}`} type="text" size="small" icon={<FileTextOutlined />} onClick={() => onLogs(project)} /></Tooltip>
          <Dropdown trigger={['click']} menu={{ items: [{ key: 'path', label: project.configFile || '未定位 Compose 文件', disabled: true }, { key: 'refresh', label: '重新发现', icon: <ReloadOutlined />, onClick: () => void onRefresh() }] }}>
            <Button aria-label={`更多 ${project.name}`} type="text" size="small" icon={<MoreOutlined />} />
          </Dropdown>
        </div>
      </div>
      {expanded ? <div className="project-compact-row__containers"><ContainerSubtable containers={project.containers} /></div> : null}
    </article>
  )
}

export function ComposePage() {
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('all')
  const [sort, setSort] = useState<ProjectSort>('issues')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(PROJECT_PAGE_SIZE)
  const [editorProject, setEditorProject] = useState<Project>()
  const [logsProject, setLogsProject] = useState<Project>()
  const [busyKey, setBusyKey] = useState('')
  const { data = [], error, loading, refresh } = useResource(api.projects, [])
  const [messageApi, contextHolder] = message.useMessage()

  const filtered = useMemo(() => sortProjects(data.filter((project) => {
    const matchesText = project.name.toLowerCase().includes(query.trim().toLowerCase())
    const matchesStatus = status === 'all' || project.status === status || (status === 'updates' && project.updateStatus === 'available')
    return matchesText && matchesStatus
  }), sort), [data, query, status, sort])
  const currentPage = Math.min(page, Math.max(1, Math.ceil(filtered.length / pageSize)))
  const visible = filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize)

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

  return (
    <section className="page compose-page">
      {contextHolder}
      <PageHeader title="Compose 项目" description="每 24 小时自动检查镜像更新；发现新版后由你确认是否更新。" />
      <div className="table-toolbar compose-toolbar">
        <Space className="compose-toolbar__filters" size={10}>
          <Input allowClear className="search-input" prefix={<SearchOutlined />} placeholder="搜索项目名称、路径或描述…" value={query} onChange={(event) => { setQuery(event.target.value); setPage(1) }} />
          <Select value={status} onChange={(value) => { setStatus(value); setPage(1) }} options={[{ value: 'all', label: '全部状态' }, { value: 'running', label: '运行中' }, { value: 'stopped', label: '已停止' }, { value: 'degraded', label: '异常' }, { value: 'updates', label: '有更新' }]} />
        </Space>
        <div className="project-sort-control">
          <span>排序</span>
          <Segmented aria-label="排序方式" size="middle" value={sort} options={PROJECT_SORT_OPTIONS} onChange={(value) => { setSort(value as ProjectSort); setPage(1) }} />
        </div>
        <Space size={10}><span className="project-count">共 {filtered.length} 项</span><Button icon={<ReloadOutlined />} onClick={() => void refresh()} loading={loading}>刷新</Button></Space>
      </div>
      {error ? <Alert className="inline-alert" type="warning" showIcon title="无法读取 Docker 数据" description={error.message} action={<Button size="small" onClick={() => void refresh()}>重试</Button>} /> : null}
      <Spin spinning={loading}>
        {visible.length ? <div className="compose-project-grid">{visible.map((project) => <ProjectCompactRow key={project.key} project={project} busyKey={busyKey} onAction={runAction} onUpdate={startUpdate} onEdit={setEditorProject} onLogs={setLogsProject} onRefresh={refresh} />)}</div> : <Empty className="compose-empty" description="没有符合条件的 Compose 项目" />}
      </Spin>
      {filtered.length > pageSize ? <Pagination className="project-pagination" current={currentPage} pageSize={pageSize} total={filtered.length} showSizeChanger pageSizeOptions={[20, 30, 40, 50]} showTotal={(total) => `共 ${total} 项`} onChange={(nextPage, nextPageSize) => { setPage(nextPageSize === pageSize ? nextPage : 1); setPageSize(nextPageSize) }} /> : null}
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
