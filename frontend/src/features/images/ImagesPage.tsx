import { useCallback, useMemo, useState } from 'react'
import { DeleteOutlined, ReloadOutlined, SearchOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { Alert, Button, Descriptions, Input, Modal, Select, Space, Table, Tag, Tooltip, Typography, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { useResource } from '../../hooks/useResource'
import type { ImageReference } from '../../types'
import { formatBytes, formatDate, shortDigest } from '../../utils/format'

type ImageFilter = 'all' | 'running' | 'stopped' | 'compose' | 'removable' | 'dangling'
type ImageUsage = { running: Set<string>; stopped: Set<string>; compose: Set<string> }

const categoryLabels: Record<string, string> = {
  'in-use': '运行中使用',
  'compose-referenced': 'Compose 已引用',
  'old-version': '异常/已停止使用',
  unused: '未使用',
  dangling: '悬空镜像',
  residual: '疑似更新残留',
}

const filterOptions: { value: ImageFilter; label: string }[] = [
  { value: 'all', label: '全部镜像' },
  { value: 'running', label: '运行中使用' },
  { value: 'stopped', label: '异常/已停止使用' },
  { value: 'compose', label: 'Compose 引用' },
  { value: 'removable', label: '多余/可删除' },
  { value: 'dangling', label: '悬空镜像' },
]

function emptyUsage(): ImageUsage {
  return { running: new Set<string>(), stopped: new Set<string>(), compose: new Set<string>() }
}

function usageCount(usage: ImageUsage) {
  return usage.running.size + usage.stopped.size + usage.compose.size
}

function matchesFilter(image: ImageReference, usage: ImageUsage, filter: ImageFilter) {
  switch (filter) {
    case 'running': return usage.running.size > 0
    case 'stopped': return usage.stopped.size > 0
    case 'compose': return usage.compose.size > 0
    case 'removable': return usageCount(usage) === 0
    case 'dangling': return image.category === 'dangling' || image.repository === '<none>'
    default: return true
  }
}

function ReferenceTag({ references, color, emptyLabel }: { references: Set<string>; color: string; emptyLabel: string }) {
  const names = [...references]
  return (
    <Tooltip title={names.length ? names.join('、') : emptyLabel}>
      <Tag color={names.length ? color : 'default'}>{names.length}</Tag>
    </Tooltip>
  )
}

function referenceDescription(references: Set<string>) {
  const names = [...references]
  return names.length ? `${names.length}：${names.join('、')}` : '0'
}

function imageRowKey(image: ImageReference) {
  return `${image.id}:${image.repository}:${image.tag}`
}

function uniqueImages(images: ImageReference[]) {
  return [...new Map(images.map((image) => [image.id, image])).values()]
}

function imageLabel(image: ImageReference) {
  return image.repository === '<none>' && image.tag === '<none>' ? shortDigest(image.id) : `${image.repository}:${image.tag}`
}

export function ImagesPage() {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<ImageFilter>('all')
  const [selectedRowKeys, setSelectedRowKeys] = useState<string[]>([])
  const [deleteCandidates, setDeleteCandidates] = useState<ImageReference[]>([])
  const [deleteError, setDeleteError] = useState('')
  const [deleteProgress, setDeleteProgress] = useState({ done: 0, total: 0 })
  const [deleting, setDeleting] = useState(false)
  const { data = [], error, loading, refresh } = useResource(api.images, [])
  const [messageApi, contextHolder] = message.useMessage()

  const images = useMemo(() => data.map((image) => ({
    ...image,
    runningReferences: image.runningReferences ?? [],
    stoppedReferences: image.stoppedReferences ?? [],
    composeReferences: image.composeReferences ?? [],
    reclaimableBytes: image.reclaimableBytes ?? 0,
  })), [data])

  const usageByID = useMemo(() => {
    const result = new Map<string, ImageUsage>()
    for (const image of images) {
      const usage = result.get(image.id) ?? emptyUsage()
      image.runningReferences.forEach((name) => usage.running.add(name))
      image.stoppedReferences.forEach((name) => usage.stopped.add(name))
      image.composeReferences.forEach((name) => usage.compose.add(name))
      result.set(image.id, usage)
    }
    return result
  }, [images])

  const filtered = useMemo(() => images.filter((image) => {
    const usage = usageByID.get(image.id) ?? emptyUsage()
    const matchesQuery = `${image.repository}:${image.tag}`.toLowerCase().includes(query.trim().toLowerCase())
    return matchesQuery && matchesFilter(image, usage, filter)
  }), [filter, images, query, usageByID])

  const metrics = useMemo(() => {
    const count = (value: ImageFilter) => images.filter((image) => matchesFilter(image, usageByID.get(image.id) ?? emptyUsage(), value)).length
    return {
      all: images.length,
      running: count('running'),
      stopped: count('stopped'),
      compose: count('compose'),
      removable: count('removable'),
      dangling: count('dangling'),
    }
  }, [images, usageByID])

  const reclaimable = useMemo(() => {
    const seen = new Set<string>()
    return images.reduce((total, image) => {
      const usage = usageByID.get(image.id) ?? emptyUsage()
      if (seen.has(image.id) || usageCount(usage) > 0) return total
      seen.add(image.id)
      return total + (image.reclaimableBytes || image.size)
    }, 0)
  }, [images, usageByID])

  const imageByKey = useMemo(() => new Map(images.map((image) => [imageRowKey(image), image])), [images])
  const selectedImages = useMemo(() => selectedRowKeys.flatMap((key) => {
    const image = imageByKey.get(key)
    return image ? [image] : []
  }), [imageByKey, selectedRowKeys])
  const selectedUniqueImages = useMemo(() => uniqueImages(selectedImages), [selectedImages])
  const safeFilteredImages = useMemo(() => filtered
    .filter((image) => usageCount(usageByID.get(image.id) ?? emptyUsage()) === 0), [filtered, usageByID])
  const safeFilteredKeys = useMemo(() => safeFilteredImages.map(imageRowKey), [safeFilteredImages])
  const safeFilteredCount = useMemo(() => uniqueImages(safeFilteredImages).length, [safeFilteredImages])
  const uniqueDeleteCandidates = useMemo(() => uniqueImages(deleteCandidates), [deleteCandidates])
  const blockedCandidates = useMemo(() => uniqueDeleteCandidates.filter((image) => usageCount(usageByID.get(image.id) ?? emptyUsage()) > 0), [uniqueDeleteCandidates, usageByID])
  const deleteBytes = useMemo(() => uniqueDeleteCandidates.reduce((total, image) => total + (image.reclaimableBytes || image.size), 0), [uniqueDeleteCandidates])
  const singleCandidate = uniqueDeleteCandidates.length === 1 ? uniqueDeleteCandidates[0] : undefined
  const candidateUsage = singleCandidate ? usageByID.get(singleCandidate.id) ?? emptyUsage() : emptyUsage()
  const hasReferences = blockedCandidates.length > 0

  const openDelete = useCallback((candidates: ImageReference[]) => {
    setDeleteError('')
    setDeleteProgress({ done: 0, total: 0 })
    setDeleteCandidates(uniqueImages(candidates))
  }, [])

  const remove = async () => {
    if (!uniqueDeleteCandidates.length || hasReferences) return
    setDeleting(true)
    setDeleteError('')
    setDeleteProgress({ done: 0, total: uniqueDeleteCandidates.length })
    const failed: { image: ImageReference; reason: string }[] = []
    const deletedIDs = new Set<string>()
    try {
      for (const [index, image] of uniqueDeleteCandidates.entries()) {
        try {
          await api.deleteImage(image.id)
          deletedIDs.add(image.id)
        } catch (reason) {
          failed.push({ image, reason: reason instanceof Error ? reason.message : '删除失败' })
        }
        setDeleteProgress({ done: index + 1, total: uniqueDeleteCandidates.length })
      }
      setSelectedRowKeys((keys) => keys.filter((key) => {
        const image = imageByKey.get(key)
        return image ? !deletedIDs.has(image.id) : false
      }))
      await refresh()
      if (failed.length) {
        setDeleteCandidates(failed.map((item) => item.image))
        setDeleteError(failed.map((item) => `${item.image.repository}:${item.image.tag}：${item.reason}`).join('\n'))
        messageApi.warning(`已删除 ${deletedIDs.size} 个，失败 ${failed.length} 个`)
      } else {
        messageApi.success(`已删除 ${deletedIDs.size} 个镜像`)
        setDeleteCandidates([])
      }
    } finally {
      setDeleting(false)
    }
  }

  const columns = useMemo<ColumnsType<ImageReference>>(() => [
    { title: '镜像仓库', dataIndex: 'repository', width: 210, fixed: 'left', sorter: (a, b) => a.repository.localeCompare(b.repository), render: (value) => <strong>{value === '<none>' ? '无标签仓库' : value}</strong> },
    { title: '标签', dataIndex: 'tag', width: 100, render: (value) => value === '<none>' ? '无标签' : value },
    { title: '镜像 ID', dataIndex: 'id', width: 130, render: shortDigest },
    { title: '摘要', dataIndex: 'digest', width: 160, ellipsis: true, render: shortDigest },
    { title: '大小', dataIndex: 'size', width: 88, sorter: (a, b) => a.size - b.size, render: formatBytes },
    { title: '创建时间', dataIndex: 'createdAt', width: 120, render: formatDate },
    { title: '运行中', width: 82, align: 'center', render: (_, image) => <ReferenceTag references={usageByID.get(image.id)?.running ?? new Set()} color="green" emptyLabel="无运行容器引用" /> },
    { title: '异常/停止', width: 92, align: 'center', render: (_, image) => <ReferenceTag references={usageByID.get(image.id)?.stopped ?? new Set()} color="orange" emptyLabel="无异常或停止容器引用" /> },
    { title: 'Compose 引用', width: 96, align: 'center', render: (_, image) => <ReferenceTag references={usageByID.get(image.id)?.compose ?? new Set()} color="blue" emptyLabel="无 Compose 引用" /> },
    { title: '更新', dataIndex: 'updateStatus', width: 104, render: (value) => value === 'available' ? <Tag color="gold">有更新</Tag> : value === 'current' ? <Tag color="green">最新</Tag> : <Tag>等待检查</Tag> },
    { title: '分类', dataIndex: 'category', width: 132, render: (value) => <Tag>{categoryLabels[value] ?? value}</Tag> },
    { title: '操作', fixed: 'right', width: 80, render: (_, image) => <Button danger type="text" size="small" icon={<DeleteOutlined />} onClick={() => openDelete([image])}>删除</Button> },
  ], [openDelete, usageByID])

  const selectFilter = (value: ImageFilter) => setFilter(value)

  return (
    <section className="page image-page">
      {contextHolder}
      <PageHeader
        title="镜像管理"
        description="按真实引用关系分类；只检查、不自动清理，删除前服务端会再次确认。"
        action={<div className="reclaim-summary"><SafetyCertificateOutlined /><span>预计可释放</span><strong>{formatBytes(reclaimable)}</strong></div>}
      />
      <div className="image-summary" aria-label="镜像分类概览">
        {filterOptions.map((option) => (
          <button key={option.value} className={filter === option.value ? 'is-active' : ''} onClick={() => selectFilter(option.value)}>
            <strong className={`image-summary--${option.value}`}>{metrics[option.value]}</strong>
            <span>{option.label}</span>
          </button>
        ))}
      </div>
      <div className="table-toolbar image-table-toolbar">
        <Space>
          <Input allowClear className="search-input" prefix={<SearchOutlined />} placeholder="搜索镜像仓库或标签…" value={query} onChange={(event) => setQuery(event.target.value)} />
          <Select<ImageFilter> value={filter} onChange={selectFilter} options={filterOptions} />
        </Space>
        <Space>
          <Typography.Text type="secondary">已选 {selectedUniqueImages.length} 个</Typography.Text>
          <Button disabled={!safeFilteredKeys.length || deleting} onClick={() => setSelectedRowKeys(safeFilteredKeys)}>全选可删除（{safeFilteredCount}）</Button>
          <Button disabled={!selectedRowKeys.length || deleting} onClick={() => setSelectedRowKeys([])}>清空</Button>
          <Button danger type="primary" icon={<DeleteOutlined />} disabled={!selectedUniqueImages.length || deleting} onClick={() => openDelete(selectedUniqueImages)}>批量删除</Button>
          <Button icon={<ReloadOutlined />} onClick={() => void refresh()} loading={loading}>刷新数据</Button>
        </Space>
      </div>
      {error ? <Alert className="inline-alert" type="warning" showIcon title="镜像数据不可用" description={error.message} /> : null}
      <Table<ImageReference>
        className="dense-table"
        rowKey={imageRowKey}
        size="small"
        columns={columns}
        dataSource={filtered}
        loading={loading}
        rowSelection={{
          selectedRowKeys,
          columnWidth: 38,
          fixed: true,
          onChange: (keys) => setSelectedRowKeys(keys.map(String)),
          getCheckboxProps: (image) => {
            const referenced = usageCount(usageByID.get(image.id) ?? emptyUsage()) > 0
            return { disabled: referenced, title: referenced ? '仍有容器或 Compose 引用，不能删除' : '选择此镜像' }
          },
        }}
        scroll={{ x: 1400 }}
        pagination={{ pageSize: 20, showSizeChanger: true, pageSizeOptions: [15, 20, 30, 50], showTotal: (total) => `共 ${total} 个镜像引用` }}
      />
      <Modal
        open={Boolean(uniqueDeleteCandidates.length)}
        title={uniqueDeleteCandidates.length > 1 ? `批量删除 ${uniqueDeleteCandidates.length} 个镜像` : '删除镜像前安全检查'}
        okText={deleting ? `删除中 ${deleteProgress.done}/${deleteProgress.total}` : '确认删除'}
        cancelText="取消"
        okButtonProps={{ danger: true, disabled: hasReferences, loading: deleting }}
        cancelButtonProps={{ disabled: deleting }}
        closable={!deleting}
        maskClosable={!deleting}
        onOk={() => void remove()}
        onCancel={() => { if (!deleting) setDeleteCandidates([]) }}
      >
        {singleCandidate ? <>
          <Typography.Paragraph><strong>{singleCandidate.repository}:{singleCandidate.tag}</strong><br />大小：{formatBytes(singleCandidate.size)}</Typography.Paragraph>
          <Descriptions bordered size="small" column={1} items={[
            { key: 'running', label: '运行容器引用', children: referenceDescription(candidateUsage.running) },
            { key: 'stopped', label: '异常/停止容器引用', children: referenceDescription(candidateUsage.stopped) },
            { key: 'compose', label: 'Compose 引用', children: referenceDescription(candidateUsage.compose) },
          ]} />
        </> : <>
          <Typography.Paragraph>将删除 <strong>{uniqueDeleteCandidates.length}</strong> 个无引用镜像，预计释放 <strong>{formatBytes(deleteBytes)}</strong>。</Typography.Paragraph>
          <Space size={[4, 4]} wrap>
            {uniqueDeleteCandidates.slice(0, 12).map((image) => <Tag key={image.id}>{imageLabel(image)}</Tag>)}
            {uniqueDeleteCandidates.length > 12 ? <Tag>另 {uniqueDeleteCandidates.length - 12} 个</Tag> : null}
          </Space>
        </>}
        {hasReferences
          ? <Alert className="modal-alert" type="error" showIcon title="当前不可安全删除" description={`${blockedCandidates.length} 个镜像仍存在引用。服务端执行时也会按 Image ID 汇总所有 Tag 并拒绝删除。`} />
          : <Alert className="modal-alert" type="success" showIcon title="可以安全删除" description="此操作不可撤销；每个镜像删除前，服务端都会再次核对运行容器、停止容器和 Compose 引用。" />}
        {deleteError ? <Alert className="modal-alert" type="warning" showIcon title="部分镜像删除失败" description={<Typography.Text style={{ whiteSpace: 'pre-line' }}>{deleteError}</Typography.Text>} /> : null}
      </Modal>
    </section>
  )
}
