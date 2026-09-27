import { useEffect, useState } from 'react'
import { Alert, Button, Empty, Input, Modal, Select, Space, Spin, Table, Tag, Typography } from 'antd'
import { LinkOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../../api/client'
import type { Project, WebAccessCandidate, WebAccessConfig, WebAccessResult } from '../../types'

const statuses: Record<WebAccessCandidate['status'], { label: string; color?: string }> = {
  web: { label: '检测到网页', color: 'green' },
  auth: { label: '需要登录 / 访问受限', color: 'orange' },
  redirect: { label: 'HTTP 跳转，待确认', color: 'blue' },
  http: { label: 'HTTP 响应，未确认网页' },
  certificate: { label: 'HTTPS 证书需确认', color: 'orange' },
  unreachable: { label: '未检测到网页' },
  stopped: { label: '容器已停止' },
}

function portKey(config: WebAccessConfig) { return JSON.stringify([config.service, config.privatePort, config.publicPort]) }

export function WebAccessModal({ project, onClose, onSaved }: { project: Project; onClose: () => void; onSaved: () => Promise<void> }) {
  const [result, setResult] = useState<WebAccessResult>()
  const [config, setConfig] = useState<WebAccessConfig>()
  const [attempt, setAttempt] = useState(0)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [savedMessage, setSavedMessage] = useState(false)

  useEffect(() => {
    let cancelled = false
    api.projectWebAccess(project.key).then((value) => {
      if (cancelled) return
      setResult(value)
      const saved = value.saved && value.candidates.find((candidate) => portKey(candidate) === portKey(value.saved!))
      const recommended = value.candidates.find((candidate) => candidate.url === value.recommendedUrl)
      setConfig(saved ? value.saved : recommended)
    }).catch((err: Error) => { if (!cancelled) setError(err.message) }).finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [project.key, attempt])

  const selected = result?.candidates.find((candidate) => config && portKey(candidate) === portKey(config))
  const validPath = Boolean(config && config.path.startsWith('/') && !config.path.startsWith('//') && !config.path.includes('\\') && !Array.from(config.path).some((char) => char.charCodeAt(0) < 32) && config.path.length <= 1024)
  const accessURL = selected && config && validPath ? `${config.scheme}://${new URL(selected.url).host}${config.path}` : ''

  async function save() {
    if (!config || !accessURL) return
    setSaving(true)
    setError('')
    try {
      const { service, privatePort, publicPort, scheme, path } = config
      await api.saveProjectWebAccess(project.key, { service, privatePort, publicPort, scheme, path })
      setSavedMessage(true)
      await onSaved()
    } catch (err) { setError((err as Error).message) }
    finally { setSaving(false) }
  }

  return <Modal open title={`${project.name} · 网页访问`} onCancel={onClose} width={760} footer={<Space wrap>
    <Button onClick={onClose}>关闭</Button>
    <Button icon={<ReloadOutlined />} disabled={saving} loading={loading} onClick={() => { setLoading(true); setError(''); setSavedMessage(false); setAttempt((value) => value + 1) }}>重新检测</Button>
    <Button disabled={!accessURL || loading} loading={saving} onClick={() => void save()}>保存为默认入口</Button>
    <Button type="primary" icon={<LinkOutlined />} href={accessURL || undefined} target="_blank" rel="noreferrer" disabled={!accessURL || loading}>打开网页</Button>
  </Space>}>
    <Typography.Paragraph type="secondary">只检查当前项目的 TCP 映射端口，自动尝试 HTTP/HTTPS。只有一个明确的网页时自动选中；多个入口请先选择。端口号相同不代表用途相同。</Typography.Paragraph>
    {error ? <Alert type="error" showIcon title={error} /> : null}
    {savedMessage ? <Alert type="success" showIcon title="默认入口已保存，其他电脑登录后同样生效。" /> : null}
    {result?.saved && !result.candidates.some((candidate) => portKey(candidate) === portKey(result.saved!)) ? <Alert type="warning" showIcon title="原默认端口映射已不存在，请重新选择。" /> : null}
    <Spin spinning={loading} tip="正在检测网页端口…">
      <Table<WebAccessCandidate> size="small" rowKey={portKey} dataSource={result?.candidates ?? []} pagination={false} scroll={{ x: 580 }}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={loading ? '正在读取端口…' : '没有可访问的 TCP 映射端口；host 网络或未发布端口的服务无法在此自动识别。'} /> }}
        rowSelection={{ type: 'radio', selectedRowKeys: config ? [portKey(config)] : [], onChange: (_, rows) => { setConfig(rows[0]); setSavedMessage(false) }, getCheckboxProps: (candidate) => ({ disabled: !candidate.url }) }}
        columns={[
          { title: '容器 / 服务', dataIndex: 'container', ellipsis: true },
          { title: '宿主机 → 容器', width: 155, render: (_, candidate) => `${candidate.publicPort} → ${candidate.privatePort}` },
          { title: '检测结果', width: 220, render: (_, candidate) => <Tag color={statuses[candidate.status].color}>{statuses[candidate.status].label}{candidate.httpStatus ? ` (${candidate.httpStatus})` : ''}</Tag> },
        ]} />
    </Spin>
    {selected && config ? <div style={{ marginTop: 16 }}>
      <Space.Compact style={{ width: '100%' }}>
        <Select aria-label="网页访问协议" value={config.scheme} style={{ width: 110 }} options={[{ value: 'http', label: 'HTTP' }, { value: 'https', label: 'HTTPS' }]} onChange={(scheme) => { setConfig({ ...config, scheme }); setSavedMessage(false) }} />
        <Input aria-label="网页访问路径" value={config.path} placeholder="/ 或 /transmission/web/" status={validPath ? undefined : 'error'} onChange={(event) => { setConfig({ ...config, path: event.target.value }); setSavedMessage(false) }} />
      </Space.Compact>
      <Typography.Paragraph style={{ marginTop: 10, overflowWrap: 'anywhere' }}>{accessURL || '请输入以 / 开头的站内路径'}</Typography.Paragraph>
      {selected.status !== 'web' ? <Alert type="info" showIcon title="此端口尚未确认是可用网页。可调整协议、路径并试打开，确认正确后再保存。" /> : null}
    </div> : null}
  </Modal>
}
