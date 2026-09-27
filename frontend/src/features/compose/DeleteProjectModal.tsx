import { useEffect, useState } from 'react'
import { Alert, Button, Checkbox, Input, Modal, Space, Spin, Table, Tag, Typography } from 'antd'
import { api } from '../../api/client'
import type { Project, RemovalItem, RemovalPlan, RemovalResult } from '../../types'

const kinds = { container: '容器', image: '镜像', volume: '数据卷' }

export function DeleteProjectModal({ project, onClose, onDeleted }: { project: Project; onClose: () => void; onDeleted: () => Promise<void> }) {
  const [plan, setPlan] = useState<RemovalPlan>()
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const [name, setName] = useState('')
  const [acknowledged, setAcknowledged] = useState(false)
  const [directory, setDirectory] = useState(false)
  const [images, setImages] = useState(false)
  const [volumes, setVolumes] = useState(false)
  const [result, setResult] = useState<RemovalResult>()
  useEffect(() => {
    let cancelled = false
    api.previewRemoval(project.key).then((value) => { if (!cancelled) setPlan(value) })
      .catch((err: Error) => { if (!cancelled) setError(err.message) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [project.key, attempt])

  function reloadPreview() {
    setPlan(undefined); setResult(undefined); setError(''); setName(''); setAcknowledged(false)
    setDirectory(false); setImages(false); setVolumes(false); setLoading(true); setAttempt((value) => value + 1)
  }
  async function remove() {
    if (!plan || busy || name !== plan.name || !acknowledged) return
    setBusy(true); setError('')
    try {
      const value = await api.deleteProject(project.key, { token: plan.token, name, directory, images, volumes })
      setResult(value)
      try { await onDeleted() } catch { setError('删除结果已返回，但项目列表刷新失败；请关闭后刷新页面查看。') }
    } catch (err) { setError(err instanceof Error ? err.message : '删除请求失败，请刷新预览确认实际状态'); setPlan(undefined) }
    finally { setBusy(false) }
  }
  function outcome(item: RemovalItem) {
    if (item.reason) return <Typography.Text type="secondary">保留：{item.reason}</Typography.Text>
    if (item.kind === 'container' || (item.kind === 'image' && images) || (item.kind === 'volume' && volumes)) return <Tag color="red">将删除</Tag>
    return '保留（未勾选）'
  }
  return <Modal open title={`删除项目 · ${project.name}`} width={760} onCancel={onClose} closable={!busy} maskClosable={false} keyboard={!busy} footer={[
    <Button key="close" disabled={busy} onClick={onClose}>{result ? '关闭' : '取消'}</Button>,
    !result ? <Button key="preview" disabled={busy || loading} onClick={reloadPreview}>重新预览</Button> : null,
    !result ? <Button key="delete" danger type="primary" loading={busy} disabled={loading || !plan || !acknowledged || name !== plan.name} onClick={() => void remove()}>确认删除</Button> : null,
  ]}>
    <Spin spinning={loading || busy} tip={busy ? '正在停止容器、备份配置并执行删除，请勿关闭页面…' : '正在检查引用…'}>
      <Space orientation="vertical" size={12} style={{ width: '100%' }}>
        {error ? <Alert type="error" showIcon title={error} /> : null}
        {result ? <>
          <Alert showIcon type={result.errors.length ? 'warning' : 'success'} title={result.errors.length ? '删除未全部完成，请查看明细；不会自动重试' : '已完成所选删除操作'} description="保留项没有删除；应用数据和已删除的数据卷不能通过配置备份恢复。" />
          <Typography.Text>Compose 配置备份：{result.backup || '未创建'}</Typography.Text>
          {result.deleted.map((text, i) => <div key={`deleted-${i}`}>已删除：{text}</div>)}
          {result.retained.map((text, i) => <div key={`retained-${i}`}>保留：{text}</div>)}
          {result.errors.map((text, i) => <Typography.Text type="danger" key={`error-${i}`}>{text}</Typography.Text>)}
        </> : plan ? <>
          <Alert type="warning" showIcon title="这是真实删除，不是隐藏项目" description="关联容器会停止并移除。只自动备份 Compose 文件，不备份数据库、下载文件或数据卷。共享镜像、外部数据及网络保留。" />
          <Typography.Text style={{ overflowWrap: 'anywhere' }}>必删 Compose 文件：{plan.hostFile || plan.file}</Typography.Text>
          <Checkbox checked={directory} disabled={Boolean(plan.directoryBlocked)} onChange={(event) => setDirectory(event.target.checked)}>同时永久删除项目目录及全部文件</Checkbox>
          <Typography.Text type={plan.directoryBlocked ? 'warning' : 'secondary'} style={{ overflowWrap: 'anywhere' }}>{plan.hostDirectory || plan.directory}{plan.directoryBlocked ? ` — ${plan.directoryBlocked}` : '（含 .env、配置、数据库、下载文件等；不会清理目录外挂载）'}</Typography.Text>
          <Checkbox checked={images} onChange={(event) => setImages(event.target.checked)}>同时删除仅此项目使用的镜像</Checkbox>
          <Checkbox checked={volumes} onChange={(event) => setVolumes(event.target.checked)}>同时永久删除该项目独占的数据卷（不可恢复）</Checkbox>
          <Table<RemovalItem> size="small" rowKey={(item) => `${item.kind}:${item.id}`} dataSource={plan.items} pagination={false} scroll={{ y: 220 }} columns={[
            { title: '类型', dataIndex: 'kind', width: 70, render: (kind: RemovalItem['kind']) => kinds[kind] },
            { title: '资源', dataIndex: 'name', ellipsis: true },
            { title: '处理方式', width: 245, render: (_, item) => outcome(item) },
          ]} />
          {plan.retained.map((text, index) => <Typography.Text type="secondary" key={index} style={{ overflowWrap: 'anywhere' }}>{text}</Typography.Text>)}
          <Checkbox checked={acknowledged} onChange={(event) => setAcknowledged(event.target.checked)}>我已核对路径和删除范围，了解数据无法恢复</Checkbox>
          <Input aria-label="确认删除项目名称" autoComplete="off" placeholder={`请输入项目名称：${plan.name}`} value={name} onChange={(event) => setName(event.target.value)} />
        </> : null}
      </Space>
    </Spin>
  </Modal>
}
