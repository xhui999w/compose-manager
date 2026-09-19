import { lazy, Suspense, useEffect, useState } from 'react'
import { CheckCircleOutlined, ClockCircleOutlined, DiffOutlined, SaveOutlined } from '@ant-design/icons'
import { Alert, Button, Drawer, List, Popconfirm, Skeleton, Space, Tabs, Tag, Typography, message } from 'antd'
import { parse } from 'yaml'
import { api } from '../../api/client'
import type { ComposeFile, ComposeVersion, Project } from '../../types'
import { formatDate } from '../../utils/format'

const MonacoEditor = lazy(() => import('./MonacoEditor'))

export function ComposeEditorDrawer({ project, open, onClose, onSaved }: { project?: Project; open: boolean; onClose: () => void; onSaved: () => Promise<unknown> }) {
  const [file, setFile] = useState<ComposeFile>()
  const [content, setContent] = useState('')
  const [diff, setDiff] = useState('')
  const [versions, setVersions] = useState<ComposeVersion[]>([])
  const [loading, setLoading] = useState(false)
  const [validating, setValidating] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [messageApi, contextHolder] = message.useMessage()

  useEffect(() => {
    if (!open || !project) return
    setLoading(true); setError(''); setDiff('')
    void Promise.all([api.file(project.key), api.versions(project.key)]).then(([loadedFile, loadedVersions]) => {
      setFile(loadedFile); setContent(loadedFile.content); setVersions(loadedVersions)
    }).catch((reason: Error) => setError(reason.message)).finally(() => setLoading(false))
  }, [open, project])

  const validate = async () => {
    if (!project) return
    setValidating(true); setError('')
    try {
      parse(content)
      const result = await api.validateFile(project.key, content)
      setDiff(result.diff || '没有变更。')
      messageApi.success('YAML 与 docker compose config 校验通过')
    } catch (reason) {
      setDiff(''); setError(reason instanceof Error ? reason.message : '校验失败')
    } finally { setValidating(false) }
  }

  const save = async (apply: boolean) => {
    if (!project || !file) return
    setSaving(true); setError('')
    try {
      await api.saveFile(project.key, content, file.sha256, apply)
      messageApi.success(apply ? '已备份、保存并应用' : '已备份并保存')
      const [nextFile, nextVersions] = await Promise.all([api.file(project.key), api.versions(project.key)])
      setFile(nextFile); setContent(nextFile.content); setVersions(nextVersions); setDiff('')
      await onSaved()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败') }
    finally { setSaving(false) }
  }

  const restore = async (version: ComposeVersion) => {
    if (!project || !file) return
    setSaving(true)
    try {
      await api.restore(project.key, version.id, file.sha256, false)
      messageApi.success('历史版本已恢复，当前版本已先备份')
      const next = await api.file(project.key); setFile(next); setContent(next.content); setDiff('')
    } catch (reason) { setError(reason instanceof Error ? reason.message : '恢复失败') }
    finally { setSaving(false) }
  }

  return (
    <Drawer className="editor-drawer" width="min(1120px, 94vw)" open={open} onClose={onClose} destroyOnHidden title={<div><strong>编辑 {project?.name}</strong><Typography.Text type="secondary" className="drawer-path">{file?.path}</Typography.Text></div>} extra={<Space><Button icon={<CheckCircleOutlined />} loading={validating} onClick={() => void validate()}>校验并查看 Diff</Button><Button icon={<SaveOutlined />} disabled={!diff || diff === '没有变更。'} loading={saving} onClick={() => void save(false)}>仅保存</Button><Popconfirm title="保存并应用？" description="保存前会创建备份，随后执行 docker compose up -d。" onConfirm={() => void save(true)}><Button type="primary" icon={<SaveOutlined />} disabled={!diff || diff === '没有变更。'} loading={saving}>保存并应用</Button></Popconfirm></Space>}>
      {contextHolder}
      {error ? <Alert className="inline-alert" showIcon type="error" title="操作未执行" description={error} /> : null}
      {loading ? <Skeleton active /> : (
        <Tabs defaultActiveKey="source" items={[
          { key: 'source', label: '源码', children: <Suspense fallback={<Skeleton active />}><MonacoEditor value={content} onChange={(value) => { setContent(value); setDiff('') }} /></Suspense> },
          { key: 'diff', label: <span><DiffOutlined /> Diff</span>, children: diff ? <pre className="diff-view">{diff}</pre> : <div className="empty-hint">修改后点击“校验并查看 Diff”。</div> },
          { key: 'history', label: <span><ClockCircleOutlined /> 历史版本</span>, children: <List size="small" dataSource={versions} locale={{ emptyText: '暂无历史版本' }} renderItem={(version) => <List.Item actions={[<Popconfirm key="restore" title="恢复这个版本？" description="恢复前会先备份当前文件。" onConfirm={() => void restore(version)}><Button type="link" size="small">恢复</Button></Popconfirm>]}><List.Item.Meta title={formatDate(version.createdAt)} description={<Space><Tag>{version.sha256.slice(0, 12)}</Tag><Typography.Text type="secondary">{version.filePath}</Typography.Text></Space>} /></List.Item>} /> },
        ]} />
      )}
    </Drawer>
  )
}

