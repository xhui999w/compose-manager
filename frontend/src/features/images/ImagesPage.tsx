import { useMemo, useState } from 'react'
import { DeleteOutlined, ReloadOutlined, SearchOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { Alert, Button, Descriptions, Input, Modal, Select, Space, Table, Tag, Typography, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { useResource } from '../../hooks/useResource'
import type { ImageReference } from '../../types'
import { formatBytes, formatDate, shortDigest } from '../../utils/format'

const categoryLabels: Record<string, string> = { 'in-use': '使用中', 'compose-referenced': 'Compose 已引用', 'old-version': '旧版本', unused: '未使用', dangling: 'Dangling', residual: '疑似更新残留' }

export function ImagesPage() {
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState('all')
  const [candidate, setCandidate] = useState<ImageReference>()
  const [deleting, setDeleting] = useState(false)
  const [checking, setChecking] = useState(false)
  const { data = [], error, loading, refresh } = useResource(api.images, [])
  const [messageApi, contextHolder] = message.useMessage()
  const filtered = useMemo(() => data.filter((image) => `${image.repository}:${image.tag}`.toLowerCase().includes(query.toLowerCase()) && (category === 'all' || image.category === category)), [data, query, category])
  const reclaimable = data.reduce((total, image) => total + image.reclaimableBytes, 0)
  const hasReferences = candidate ? candidate.runningReferences.length + candidate.stoppedReferences.length + candidate.composeReferences.length > 0 : false

  const remove = async () => {
    if (!candidate || hasReferences) return
    setDeleting(true)
    try { await api.deleteImage(candidate.id); messageApi.success('镜像已删除'); setCandidate(undefined); await refresh() }
    catch (reason) { messageApi.error(reason instanceof Error ? reason.message : '删除失败') }
    finally { setDeleting(false) }
  }

  const checkUpdates = async () => {
    setChecking(true)
    try {
      const checked = await api.checkImageUpdates()
      messageApi.info(`已检查 ${checked.length} 个镜像；私有仓库或限流时会保持“未检查”。`)
      await refresh()
    } catch (reason) { messageApi.error(reason instanceof Error ? reason.message : '检查失败') }
    finally { setChecking(false) }
  }

  const columns: ColumnsType<ImageReference> = [
    { title: 'Repository', dataIndex: 'repository', width: 210, fixed: 'left', sorter: (a, b) => a.repository.localeCompare(b.repository), render: (value) => <strong>{value}</strong> },
    { title: 'Tag', dataIndex: 'tag', width: 100 },
    { title: 'Image ID', dataIndex: 'id', width: 130, render: shortDigest },
    { title: 'Digest', dataIndex: 'digest', width: 160, ellipsis: true, render: shortDigest },
    { title: '大小', dataIndex: 'size', width: 88, sorter: (a, b) => a.size - b.size, render: formatBytes },
    { title: '创建时间', dataIndex: 'createdAt', width: 120, render: formatDate },
    { title: '运行容器', dataIndex: 'runningReferences', width: 92, align: 'center', render: (value: string[]) => <Tag color={value.length ? 'green' : 'default'}>{value.length}</Tag> },
    { title: '停止容器', dataIndex: 'stoppedReferences', width: 92, align: 'center', render: (value: string[]) => <Tag color={value.length ? 'orange' : 'default'}>{value.length}</Tag> },
    { title: 'Compose 引用', dataIndex: 'composeReferences', width: 106, align: 'center', render: (value: string[]) => <Tag color={value.length ? 'blue' : 'default'}>{value.length}</Tag> },
    { title: '更新', dataIndex: 'updateStatus', width: 88, render: (value) => value === 'available' ? <Tag color="gold">有更新</Tag> : value === 'current' ? <Tag>最新</Tag> : <Tag>未检查</Tag> },
    { title: '分类', dataIndex: 'category', width: 120, render: (value) => <Tag>{categoryLabels[value] ?? value}</Tag> },
    { title: '操作', fixed: 'right', width: 80, render: (_, image) => <Button danger type="text" size="small" icon={<DeleteOutlined />} onClick={() => setCandidate(image)}>删除</Button> },
  ]

  return <section className="page">{contextHolder}<PageHeader title="镜像" description="清楚查看镜像是否被运行容器、停止容器或 Compose 文件引用；不执行自动清理。" action={<div className="reclaim-summary"><SafetyCertificateOutlined /><span>预计可释放</span><strong>{formatBytes(reclaimable)}</strong></div>} /><div className="table-toolbar"><Space><Input allowClear className="search-input" prefix={<SearchOutlined />} placeholder="搜索 Repository 或 Tag…" value={query} onChange={(event) => setQuery(event.target.value)} /><Select value={category} onChange={setCategory} options={[{ value: 'all', label: '全部分类' }, ...Object.entries(categoryLabels).map(([value, label]) => ({ value, label }))]} /></Space><Space><Button onClick={() => void checkUpdates()} loading={checking}>检查更新</Button><Button icon={<ReloadOutlined />} onClick={() => void refresh()} loading={loading}>刷新</Button></Space></div>{error ? <Alert className="inline-alert" type="warning" showIcon message="镜像数据不可用" description={error.message} /> : null}<Table<ImageReference> className="dense-table" rowKey={(image) => `${image.id}:${image.repository}:${image.tag}`} size="small" columns={columns} dataSource={filtered} loading={loading} scroll={{ x: 1400 }} pagination={{ pageSize: 20, showTotal: (total) => `共 ${total} 个引用` }} /><Modal open={Boolean(candidate)} title="删除镜像前安全检查" okText="确认删除" cancelText="取消" okButtonProps={{ danger: true, disabled: hasReferences, loading: deleting }} onOk={() => void remove()} onCancel={() => setCandidate(undefined)}><Typography.Paragraph><strong>{candidate?.repository}:{candidate?.tag}</strong><br />大小：{formatBytes(candidate?.size)}</Typography.Paragraph><Descriptions bordered size="small" column={1} items={[{ key: 'running', label: '运行容器引用', children: candidate?.runningReferences.length ?? 0 }, { key: 'stopped', label: '停止容器引用', children: candidate?.stoppedReferences.length ?? 0 }, { key: 'compose', label: 'Compose 引用', children: candidate?.composeReferences.length ?? 0 }]} />{hasReferences ? <Alert className="modal-alert" type="error" showIcon message="当前不可安全删除" description="仍存在引用。服务端执行时也会重新检查并拒绝删除。" /> : <Alert className="modal-alert" type="success" showIcon message="可以安全删除" description="服务端仍会在删除前再次核对所有引用。" />}</Modal></section>
}
