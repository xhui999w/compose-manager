import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import { EditOutlined, FileOutlined, FolderAddOutlined, FolderOpenOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import { Alert, Breadcrumb, Button, Drawer, Empty, Input, Modal, Select, Skeleton, Space, Table, Tag, Tooltip, Typography, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import type { WorkspaceEntry, WorkspaceFile, WorkspaceRoot } from '../../types'
import { formatBytes, formatDate } from '../../utils/format'

const MonacoEditor = lazy(() => import('../compose/MonacoEditor'))

function joinPath(parent: string, child: string) {
  return parent ? `${parent.replace(/\/$/, '')}/${child}` : child
}

function parentPath(path: string) {
  const parts = path.split('/').filter(Boolean)
  parts.pop()
  return parts.join('/')
}

function editorLanguage(path: string) {
  const name = path.toLowerCase()
  if (name.endsWith('.yaml') || name.endsWith('.yml')) return 'yaml'
  if (name.endsWith('.json')) return 'json'
  if (name.endsWith('.ini') || name.endsWith('.conf') || name.endsWith('.cfg') || name.endsWith('.env')) return 'ini'
  return 'plaintext'
}

export function WorkspacePage() {
  const [roots, setRoots] = useState<WorkspaceRoot[]>([])
  const [rootId, setRootId] = useState(0)
  const [path, setPath] = useState('')
  const [entries, setEntries] = useState<WorkspaceEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [file, setFile] = useState<WorkspaceFile>()
  const [content, setContent] = useState('')
  const [saving, setSaving] = useState(false)
  const [folderOpen, setFolderOpen] = useState(false)
  const [folderName, setFolderName] = useState('')
  const [creatingFolder, setCreatingFolder] = useState(false)
  const [messageApi, contextHolder] = message.useMessage()

  const loadEntries = useCallback(async (nextRoot = rootId, nextPath = path) => {
    setLoading(true); setError('')
    try { setEntries(await api.workspaceEntries(nextRoot, nextPath)) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '无法读取目录') }
    finally { setLoading(false) }
  }, [path, rootId])

  useEffect(() => {
    void api.workspaceRoots().then((items) => {
      setRoots(items)
      const first = items[0]?.id ?? 0
      setRootId(first)
      return loadEntries(first, '')
    }).catch((reason) => { setError(reason instanceof Error ? reason.message : '无法读取工作区'); setLoading(false) })
  }, [])

  const navigate = (nextPath: string) => {
    setPath(nextPath)
    void loadEntries(rootId, nextPath)
  }

  const openFile = async (entry: WorkspaceEntry) => {
    setError('')
    try {
      const loaded = await api.workspaceFile(rootId, entry.path)
      setFile(loaded); setContent(loaded.content)
    } catch (reason) { setError(reason instanceof Error ? reason.message : '无法打开文件') }
  }

  const saveFile = async () => {
    if (!file) return
    setSaving(true); setError('')
    try {
      const saved = await api.saveWorkspaceFile(file.rootId, file.path, content, file.sha256)
      setFile(saved); setContent(saved.content)
      messageApi.success('已备份并保存')
      await loadEntries()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败') }
    finally { setSaving(false) }
  }

  const createFolder = async () => {
    const name = folderName.trim()
    if (!name || name === '.' || name === '..' || /[\\/]/.test(name)) {
      messageApi.error('请输入不含斜杠的文件夹名称')
      return
    }
    setCreatingFolder(true)
    try {
      await api.createWorkspaceDirectory(rootId, joinPath(path, name))
      messageApi.success('文件夹已创建')
      setFolderName(''); setFolderOpen(false)
      await loadEntries()
    } catch (reason) { messageApi.error(reason instanceof Error ? reason.message : '创建失败') }
    finally { setCreatingFolder(false) }
  }

  const columns = useMemo<ColumnsType<WorkspaceEntry>>(() => [
    { title: '名称', dataIndex: 'name', render: (value: string, entry) => <button className={`workspace-entry workspace-entry--${entry.kind}${entry.editable ? ' is-editable' : ''}`} type="button" onClick={() => entry.kind === 'directory' ? navigate(entry.path) : entry.editable ? void openFile(entry) : undefined}>{entry.kind === 'directory' ? <FolderOpenOutlined /> : <FileOutlined />}<span>{value}</span>{entry.composeFile ? <Tag color="blue">Compose</Tag> : null}</button> },
    { title: '类型', dataIndex: 'kind', width: 90, render: (value: WorkspaceEntry['kind']) => value === 'directory' ? '文件夹' : '文件' },
    { title: '大小', dataIndex: 'size', width: 100, render: (value: number, entry) => entry.kind === 'directory' ? '—' : formatBytes(value) },
    { title: '修改时间', dataIndex: 'modifiedAt', width: 150, render: formatDate },
    { title: '操作', width: 90, align: 'center', render: (_, entry) => entry.kind === 'directory' ? <Tooltip title="打开"><Button aria-label={`打开 ${entry.name}`} size="small" type="text" icon={<FolderOpenOutlined />} onClick={() => navigate(entry.path)} /></Tooltip> : <Tooltip title={entry.editable ? '编辑' : '此文件类型只读'}><Button aria-label={`编辑 ${entry.name}`} size="small" type="text" icon={<EditOutlined />} disabled={!entry.editable} onClick={() => void openFile(entry)} /></Tooltip> },
  ], [path, rootId])

  const breadcrumbItems = [{ title: <button type="button" onClick={() => navigate('')}>根目录</button> }, ...path.split('/').filter(Boolean).map((part, index, parts) => ({ title: <button type="button" onClick={() => navigate(parts.slice(0, index + 1).join('/'))}>{part}</button> }))]
  const selectedRoot = roots.find((root) => root.id === rootId)

  return (
    <section className="page workspace-page">
      {contextHolder}
      <PageHeader title="项目文件" description="浏览并编辑已挂载的 Compose 项目配置；所有路径都限制在允许的根目录内。" action={<Space><Button icon={<FolderAddOutlined />} onClick={() => setFolderOpen(true)}>新建文件夹</Button><Button icon={<ReloadOutlined />} onClick={() => void loadEntries()} loading={loading}>刷新</Button></Space>} />
      {error ? <Alert className="inline-alert" type="error" showIcon title="文件操作失败" description={error} closable onClose={() => setError('')} /> : null}
      <div className="workspace-toolbar">
        <Select aria-label="Compose 根目录" value={rootId} options={roots.map((root) => ({ value: root.id, label: `${root.name}（${root.path}）` }))} onChange={(value) => { setRootId(value); setPath(''); void loadEntries(value, '') }} />
        <Breadcrumb items={breadcrumbItems} />
        <Typography.Text type="secondary">NAS 映射目录：{selectedRoot?.path ?? '—'}</Typography.Text>
      </div>
      <Table<WorkspaceEntry> className="dense-table workspace-table" rowKey="path" size="small" loading={loading} columns={columns} dataSource={entries} pagination={false} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这个目录是空的" /> }} />
      {path ? <Button className="workspace-back" type="link" onClick={() => navigate(parentPath(path))}>返回上一级</Button> : null}

      <Drawer className="editor-drawer" width="min(1040px, 94vw)" open={Boolean(file)} onClose={() => setFile(undefined)} destroyOnHidden title={<div><strong>编辑配置文件</strong><Typography.Text type="secondary" className="drawer-path">{file?.path}</Typography.Text></div>} extra={<Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={() => void saveFile()}>备份并保存</Button>}>
        <Suspense fallback={<Skeleton active />}><MonacoEditor language={file ? editorLanguage(file.path) : 'plaintext'} value={content} onChange={setContent} /></Suspense>
      </Drawer>

      <Modal open={folderOpen} title="新建文件夹" okText="创建" cancelText="取消" confirmLoading={creatingFolder} onOk={() => void createFolder()} onCancel={() => setFolderOpen(false)}>
        <Typography.Paragraph type="secondary">将在 {path || '根目录'} 下创建文件夹。</Typography.Paragraph>
        <Input autoFocus value={folderName} placeholder="文件夹名称" onChange={(event) => setFolderName(event.target.value)} onPressEnter={() => void createFolder()} />
      </Modal>
    </section>
  )
}
