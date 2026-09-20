import { useMemo, useState } from 'react'
import { FileSearchOutlined, FileTextOutlined, PauseOutlined, PlayCircleOutlined, ReloadOutlined, SearchOutlined, SyncOutlined, UploadOutlined } from '@ant-design/icons'
import { Alert, Button, Drawer, Input, Popconfirm, Select, Space, Table, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { StateBadge } from '../../components/StateBadge'
import { useResource } from '../../hooks/useResource'
import type { Container } from '../../types'
import { formatBytes, formatDate, formatPercent } from '../../utils/format'

export function ContainersPage() {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState('all')
  const [logContainer, setLogContainer] = useState<Container>()
  const [logs, setLogs] = useState('')
  const [inspectContainer, setInspectContainer] = useState<Container>()
  const [inspect, setInspect] = useState('')
  const { data = [], error, loading, refresh } = useResource(api.containers, [])
  const [messageApi, contextHolder] = message.useMessage()
  const filtered = useMemo(() => data.filter((item) => {
    const text = `${item.name} ${item.image} ${item.project ?? ''}`.toLowerCase()
    const filterMatch = filter === 'all' || (filter === 'non-compose' ? !item.project : filter === 'stopped' ? item.state !== 'running' : item.state === filter)
    return text.includes(query.toLowerCase()) && filterMatch
  }), [data, query, filter])

  const action = async (container: Container, operation: string) => {
    try { await api.containerAction(container.id, operation); messageApi.success(`${container.name} 操作完成`); await refresh() }
    catch (reason) { messageApi.error(reason instanceof Error ? reason.message : '操作失败') }
  }

  const showLogs = async (container: Container) => {
    setLogContainer(container); setLogs('正在读取…')
    try { setLogs((await api.containerLogs(container.id)).logs) }
    catch (reason) { setLogs(reason instanceof Error ? reason.message : '日志读取失败') }
  }

  const showInspect = async (container: Container) => {
    setInspectContainer(container); setInspect('正在读取…')
    try { setInspect(JSON.stringify(await api.inspectContainer(container.id), null, 2)) }
    catch (reason) { setInspect(reason instanceof Error ? reason.message : 'Inspect 读取失败') }
  }

  const update = async (container: Container) => {
    if (!container.project) return
    try { await api.runUpdate(container.project, container.service ?? ''); messageApi.success(`${container.name} 更新完成`); await refresh() }
    catch (reason) { messageApi.error(reason instanceof Error ? reason.message : '更新失败') }
  }

  const columns: ColumnsType<Container> = [
    { title: '容器名称', dataIndex: 'name', width: 190, fixed: 'left', sorter: (a, b) => a.name.localeCompare(b.name), render: (value) => <strong>{value}</strong> },
    { title: '状态', dataIndex: 'state', width: 68, align: 'center', render: (value) => <StateBadge state={value} /> },
    { title: 'Compose 项目', dataIndex: 'project', width: 150, render: (value) => value ? <Tag color="blue">{value}</Tag> : <Tag>非 Compose</Tag> },
    { title: 'CPU', dataIndex: 'cpuPercent', width: 70, sorter: (a, b) => a.cpuPercent - b.cpuPercent, render: formatPercent },
    { title: '内存', dataIndex: 'memoryBytes', width: 88, sorter: (a, b) => a.memoryBytes - b.memoryBytes, render: formatBytes },
    { title: '镜像 / Tag', dataIndex: 'image', width: 250, ellipsis: true },
    { title: '端口', dataIndex: 'ports', width: 120, render: (ports: Container['ports']) => ports.filter((port) => port.publicPort).map((port) => `${port.publicPort}:${port.privatePort}`).join(', ') || '—' },
    { title: '启动时间', dataIndex: 'createdAt', width: 120, render: formatDate },
    { title: '操作', fixed: 'right', width: 320, render: (_, item) => <Space size={0}><Button type="text" size="small" icon={<PlayCircleOutlined />} disabled={item.state === 'running'} onClick={() => void action(item, 'start')}>启动</Button><Popconfirm title={`停止 ${item.name}？`} okButtonProps={{ danger: true }} onConfirm={() => void action(item, 'stop')}><Button danger type="text" size="small" icon={<PauseOutlined />} disabled={item.state !== 'running'}>停止</Button></Popconfirm><Button type="text" size="small" icon={<SyncOutlined />} onClick={() => void action(item, 'restart')}>重启</Button><Popconfirm title={`更新 ${item.name}？`} description="将按所属 Compose 项目拉取并重新应用。" onConfirm={() => void update(item)}><Button type="text" size="small" icon={<UploadOutlined />} disabled={!item.project}>更新</Button></Popconfirm><Button type="text" size="small" icon={<FileTextOutlined />} onClick={() => void showLogs(item)}>日志</Button><Button type="text" size="small" icon={<FileSearchOutlined />} onClick={() => void showInspect(item)}>Inspect</Button></Space> },
  ]

  return <section className="page">{contextHolder}<PageHeader title="容器" description="紧凑查看运行状态、资源、镜像和端口；非 Compose 容器也会单独标识。" /><div className="table-toolbar"><Space><Input allowClear className="search-input" prefix={<SearchOutlined />} placeholder="搜索容器或镜像…" value={query} onChange={(event) => setQuery(event.target.value)} /><Select value={filter} onChange={setFilter} options={[{ value: 'all', label: '全部' }, { value: 'running', label: '运行中' }, { value: 'stopped', label: '已停止' }, { value: 'non-compose', label: '非 Compose 容器' }]} /></Space><Button icon={<ReloadOutlined />} onClick={() => void refresh()} loading={loading}>刷新</Button></div>{error ? <Alert className="inline-alert" type="warning" showIcon title="容器数据不可用" description={error.message} /> : null}<Table<Container> className="dense-table" rowKey="id" size="small" columns={columns} dataSource={filtered} loading={loading} scroll={{ x: 1410 }} pagination={{ pageSize: 20, showSizeChanger: true, pageSizeOptions: [15, 20, 25, 50], showTotal: (total) => `共 ${total} 个容器` }} /><Drawer width="min(820px, 90vw)" open={Boolean(logContainer)} title={`${logContainer?.name ?? ''} 日志`} onClose={() => setLogContainer(undefined)}><pre className="log-view">{logs}</pre></Drawer><Drawer width="min(820px, 90vw)" open={Boolean(inspectContainer)} title={`${inspectContainer?.name ?? ''} Inspect`} onClose={() => setInspectContainer(undefined)}><pre className="log-view">{inspect}</pre></Drawer></section>
}
