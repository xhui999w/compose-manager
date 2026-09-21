import { useMemo, useState } from 'react'
import { FileTextOutlined, PauseOutlined, PlayCircleOutlined, ReloadOutlined, SearchOutlined, SyncOutlined, UploadOutlined } from '@ant-design/icons'
import { Alert, Button, Drawer, Empty, Input, Pagination, Popconfirm, Select, Spin, Tooltip, message } from 'antd'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { StateBadge } from '../../components/StateBadge'
import { useResource } from '../../hooks/useResource'
import type { Container } from '../../types'
import { formatBytes, formatPercent } from '../../utils/format'

const PAGE_SIZE = 24

type ContainerCardProps = {
  container: Container
  onAction: (container: Container, operation: string) => Promise<void>
  onLogs: (container: Container) => Promise<void>
  onUpdate: (container: Container) => Promise<void>
}

function ContainerCard({ container, onAction, onLogs, onUpdate }: ContainerCardProps) {
  const running = container.state === 'running'
  return (
    <article className={`container-card${container.updateStatus === 'available' ? ' container-card--update' : ''}`}>
      <div className="container-card__head">
        <StateBadge state={container.state} />
        <Tooltip title={container.name}><strong>{container.name}</strong></Tooltip>
        {container.updateStatus === 'available' ? <span className="container-card__update">有更新</span> : null}
      </div>
      <Tooltip title={container.image}><div className="container-card__image">{container.image}</div></Tooltip>
      <div className="container-card__usage">
        <span>CPU {formatPercent(container.cpuPercent)}</span><i />
        <span>内存 {formatBytes(container.memoryBytes)}</span>
      </div>
      <div className="container-card__actions">
        {running ? (
          <Popconfirm title={`停止 ${container.name}？`} okButtonProps={{ danger: true }} onConfirm={() => void onAction(container, 'stop')}>
            <Tooltip title="停止"><Button danger size="small" icon={<PauseOutlined />} aria-label={`停止 ${container.name}`} /></Tooltip>
          </Popconfirm>
        ) : (
          <Tooltip title="启动"><Button size="small" className="action-start" icon={<PlayCircleOutlined />} aria-label={`启动 ${container.name}`} onClick={() => void onAction(container, 'start')} /></Tooltip>
        )}
        <Tooltip title="重启"><Button size="small" icon={<SyncOutlined />} aria-label={`重启 ${container.name}`} onClick={() => void onAction(container, 'restart')} /></Tooltip>
        <Tooltip title="日志"><Button size="small" icon={<FileTextOutlined />} aria-label={`查看 ${container.name} 日志`} onClick={() => void onLogs(container)} /></Tooltip>
        {container.updateStatus === 'available' && container.project ? (
          <Popconfirm title={`更新 ${container.name}？`} description="将按所属 Compose 项目拉取并重新应用。" onConfirm={() => void onUpdate(container)}>
            <Tooltip title="确认更新"><Button size="small" className="action-update" icon={<UploadOutlined />} aria-label={`更新 ${container.name}`} /></Tooltip>
          </Popconfirm>
        ) : null}
      </div>
    </article>
  )
}

export function ContainersPage() {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState('all')
  const [project, setProject] = useState('all')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(PAGE_SIZE)
  const [logContainer, setLogContainer] = useState<Container>()
  const [logs, setLogs] = useState('')
  const { data = [], error, loading, refresh } = useResource(api.containers, [])
  const [messageApi, contextHolder] = message.useMessage()

  const projects = useMemo(() => [...new Set(data.map((item) => item.project).filter((value): value is string => Boolean(value)))].sort(), [data])
  const filtered = useMemo(() => data.filter((item) => {
    const text = `${item.name} ${item.image} ${item.project ?? ''}`.toLowerCase()
    const filterMatch = filter === 'all'
      || (filter === 'non-compose' ? !item.project
        : filter === 'stopped' ? item.state !== 'running'
          : filter === 'updates' ? item.updateStatus === 'available'
            : item.state === filter)
    return text.includes(query.toLowerCase()) && filterMatch && (project === 'all' || item.project === project)
  }).sort((a, b) => a.name.localeCompare(b.name)), [data, query, filter, project])
  const visible = filtered.slice((page - 1) * pageSize, page * pageSize)
  const metrics = useMemo(() => ({
    total: data.length,
    running: data.filter((item) => item.state === 'running').length,
    stopped: data.filter((item) => item.state !== 'running').length,
    updates: data.filter((item) => item.updateStatus === 'available').length,
  }), [data])

  const selectFilter = (value: string) => { setFilter(value); setPage(1) }
  const action = async (container: Container, operation: string) => {
    try { await api.containerAction(container.id, operation); messageApi.success(`${container.name} 操作完成`); await refresh() }
    catch (reason) { messageApi.error(reason instanceof Error ? reason.message : '操作失败') }
  }
  const showLogs = async (container: Container) => {
    setLogContainer(container); setLogs('正在读取…')
    try { setLogs((await api.containerLogs(container.id)).logs) }
    catch (reason) { setLogs(reason instanceof Error ? reason.message : '日志读取失败') }
  }
  const update = async (container: Container) => {
    if (!container.project) return
    try { await api.runUpdate(container.project, container.service ?? ''); messageApi.success(`${container.name} 已开始更新，请在左侧“更新进度”查看`) }
    catch (reason) { messageApi.error(reason instanceof Error ? reason.message : '无法开始更新') }
  }

  return (
    <section className="page containers-page">
      {contextHolder}
      <PageHeader title="容器" description="卡片保持紧凑；镜像每 24 小时自动检查，发现更新后由你确认。" />
      <div className="container-summary" aria-label="容器概览">
        <button className={filter === 'all' ? 'is-active' : ''} onClick={() => selectFilter('all')}><strong>{metrics.total}</strong><span>总容器</span></button>
        <button className={filter === 'running' ? 'is-active' : ''} onClick={() => selectFilter('running')}><strong className="summary-running">{metrics.running}</strong><span>运行中</span></button>
        <button className={filter === 'stopped' ? 'is-active' : ''} onClick={() => selectFilter('stopped')}><strong className="summary-stopped">{metrics.stopped}</strong><span>已停止</span></button>
        <button className={filter === 'updates' ? 'is-active' : ''} onClick={() => selectFilter('updates')}><strong className="summary-update">{metrics.updates}</strong><span>有更新</span></button>
      </div>
      <div className="container-toolbar">
        <Input allowClear prefix={<SearchOutlined />} placeholder="搜索容器、镜像或项目…" value={query} onChange={(event) => { setQuery(event.target.value); setPage(1) }} />
        <Select value={filter} onChange={selectFilter} options={[
          { value: 'all', label: '全部状态' }, { value: 'running', label: '运行中' }, { value: 'stopped', label: '已停止 / 异常' }, { value: 'updates', label: '有更新' }, { value: 'non-compose', label: '非 Compose 容器' },
        ]} />
        <Select value={project} onChange={(value) => { setProject(value); setPage(1) }} options={[{ value: 'all', label: '全部 Compose 项目' }, ...projects.map((value) => ({ value, label: value }))]} />
        <span className="container-toolbar__count">显示 {filtered.length} / {data.length}</span>
        <Tooltip title="刷新当前数据，不会触发镜像更新检查"><Button icon={<ReloadOutlined />} onClick={() => void refresh()} loading={loading}>刷新数据</Button></Tooltip>
      </div>
      {error ? <Alert className="inline-alert" type="warning" showIcon title="容器数据不可用" description={error.message} /> : null}
      <Spin spinning={loading}>
        {visible.length ? <div className="container-grid">{visible.map((item) => <ContainerCard key={item.id} container={item} onAction={action} onLogs={showLogs} onUpdate={update} />)}</div> : <Empty className="container-empty" description="没有符合条件的容器" />}
      </Spin>
      {filtered.length > pageSize ? <Pagination className="container-pagination" current={page} pageSize={pageSize} total={filtered.length} showSizeChanger pageSizeOptions={[18, 24, 30, 60]} showTotal={(total) => `共 ${total} 个容器`} onChange={(nextPage, nextPageSize) => { setPage(nextPageSize === pageSize ? nextPage : 1); setPageSize(nextPageSize) }} /> : null}
      <Drawer width="min(820px, 90vw)" open={Boolean(logContainer)} title={`${logContainer?.name ?? ''} 日志`} onClose={() => setLogContainer(undefined)}><pre className="log-view">{logs}</pre></Drawer>
    </section>
  )
}
