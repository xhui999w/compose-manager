import { lazy, Suspense, useEffect, useState } from 'react'
import { Alert, Form, Input, Modal, Select, Skeleton, Switch, Typography, message } from 'antd'
import { api } from '../../api/client'
import type { CreatedProject, WorkspaceRoot } from '../../types'

const MonacoEditor = lazy(() => import('./MonacoEditor'))

const DEFAULT_COMPOSE = `services:
  app:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "8080:80"
`

type CreateValues = {
  rootId: number
  name: string
  directory: string
  apply: boolean
}

export function CreateComposeModal({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (project: CreatedProject) => Promise<unknown> }) {
  const [form] = Form.useForm<CreateValues>()
  const [roots, setRoots] = useState<WorkspaceRoot[]>([])
  const [content, setContent] = useState(DEFAULT_COMPOSE)
  const [loadingRoots, setLoadingRoots] = useState(false)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')
  const [messageApi, contextHolder] = message.useMessage()

  useEffect(() => {
    if (!open) return
    setError('')
    setLoadingRoots(true)
    void api.workspaceRoots().then((items) => {
      setRoots(items)
      form.setFieldsValue({ rootId: items[0]?.id ?? 0 })
    }).catch((reason) => setError(reason instanceof Error ? reason.message : '无法读取 Compose 根目录')).finally(() => setLoadingRoots(false))
  }, [form, open])

  const create = async () => {
    setError('')
    let values: CreateValues
    try { values = await form.validateFields() } catch { return }
    setCreating(true)
    try {
      const project = await api.createProject(values.rootId, values.directory, values.name, content, values.apply)
      messageApi.success(values.apply ? '项目已创建并启动' : '项目已创建')
      await onCreated(project)
      form.resetFields()
      setContent(DEFAULT_COMPOSE)
      onClose()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '创建失败')
    } finally { setCreating(false) }
  }

  return (
    <Modal width="min(980px, 94vw)" open={open} title="新建 Compose 项目" okText="创建项目" cancelText="取消" confirmLoading={creating} onOk={() => void create()} onCancel={onClose} destroyOnHidden>
      {contextHolder}
      {error ? <Alert className="inline-alert" type="error" showIcon title="无法创建项目" description={error} /> : null}
      <Form<CreateValues> form={form} layout="vertical" initialValues={{ rootId: 0, name: '', directory: '', apply: false }} disabled={loadingRoots || creating}>
        <div className="create-compose-fields">
          <Form.Item label="保存位置" name="rootId" rules={[{ required: true, message: '请选择保存位置' }]}>
            <Select loading={loadingRoots} options={roots.map((root) => ({ value: root.id, label: `${root.name}（${root.path}）` }))} />
          </Form.Item>
          <Form.Item label="项目名称" name="name" rules={[{ required: true, message: '请输入项目名称' }, { pattern: /^[a-z0-9][a-z0-9_.-]{0,127}$/, message: '仅使用小写字母、数字、点、下划线或短横线' }]}>
            <Input placeholder="例如：my-app" />
          </Form.Item>
          <Form.Item label="相对目录" name="directory" rules={[{ required: true, message: '请输入项目目录' }, { validator: (_, value: string) => !value || (!value.startsWith('/') && !value.split(/[\\/]+/).includes('..')) ? Promise.resolve() : Promise.reject(new Error('必须是根目录内的相对路径')) }]}>
            <Input placeholder="例如：my-app 或 media/my-app" />
          </Form.Item>
          <Form.Item className="create-compose-apply" label="创建后启动" name="apply" valuePropName="checked">
            <Switch checkedChildren="启动" unCheckedChildren="仅保存" />
          </Form.Item>
        </div>
        <Typography.Paragraph className="create-compose-help" type="secondary">系统会先执行 YAML 与 docker compose config 校验；校验失败不会留下 Compose 文件。项目目录必须不存在，防止覆盖已有数据。</Typography.Paragraph>
        <Form.Item label="compose.yaml">
          <div className="create-compose-editor"><Suspense fallback={<Skeleton active />}><MonacoEditor height="380px" value={content} onChange={setContent} /></Suspense></div>
        </Form.Item>
      </Form>
    </Modal>
  )
}
