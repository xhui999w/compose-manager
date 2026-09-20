import { useEffect, useState } from 'react'
import { Alert, Button, Divider, Form, Input, Radio, Select, Space, Switch, Typography, message } from 'antd'
import { SaveOutlined } from '@ant-design/icons'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { useResource } from '../../hooks/useResource'

type SettingsValues = {
  dockerHost: string
  composeRoots: string
  nasIP: string
  defaultScheme: 'http' | 'https'
  externalURL: string
  density: 'compact' | 'comfortable'
}

const defaults: SettingsValues = { dockerHost: 'unix:///var/run/docker.sock', composeRoots: '/compose', nasIP: '192.168.1.100', defaultScheme: 'http', externalURL: '', density: 'compact' }

export function SettingsPage({ dark, onThemeChange }: { dark: boolean; onThemeChange: (dark: boolean) => void }) {
  const [form] = Form.useForm<SettingsValues>()
  const [saving, setSaving] = useState(false)
  const { data, error, loading } = useResource(api.settings, [])
  const [messageApi, contextHolder] = message.useMessage()
  useEffect(() => { form.setFieldsValue({ ...defaults, ...(data as Partial<SettingsValues> | undefined) }) }, [data, form])
  const save = async (values: SettingsValues) => {
    setSaving(true)
    try { await api.putSettings(values); localStorage.setItem('cm-density', values.density); document.documentElement.dataset.density = values.density; messageApi.success('设置已保存；连接与扫描目录变更需重启服务生效。') }
    catch (reason) { messageApi.error(reason instanceof Error ? reason.message : '保存失败') }
    finally { setSaving(false) }
  }
  return <section className="page settings-page">{contextHolder}<PageHeader title="设置" description="保持必要配置简单明确。Docker Socket 拥有极高权限，请勿将面板直接暴露到公网。" />{error ? <Alert className="inline-alert" type="warning" showIcon title="设置读取失败" description={error.message} /> : null}<Form<SettingsValues> form={form} layout="horizontal" labelCol={{ span: 6 }} wrapperCol={{ span: 14 }} initialValues={defaults} onFinish={(values) => void save(values)} disabled={loading}><div className="settings-section"><h2>Docker 与 Compose</h2><Form.Item label="Docker Socket" name="dockerHost" rules={[{ required: true }]} extra="浏览器不会直接访问该地址；仅后端使用。"><Input placeholder="unix:///var/run/docker.sock" /></Form.Item><Form.Item label="Compose 扫描目录" name="composeRoots" rules={[{ required: true }]} extra="多个目录用逗号分隔；文件访问严格限制在这些目录内。"><Input placeholder="/compose,/volume1/docker" /></Form.Item><Form.Item label="NAS 内网 IP" name="nasIP" rules={[{ required: true }]}><Input placeholder="192.168.1.100" /></Form.Item></div><Divider /><div className="settings-section"><h2>更新与访问</h2><Form.Item label="镜像更新检查"><Typography.Text>每 24 小时自动检查，只提示结果；升级必须由用户确认。</Typography.Text></Form.Item><Form.Item label="默认协议" name="defaultScheme"><Select options={[{ value: 'http', label: 'HTTP' }, { value: 'https', label: 'HTTPS' }]} /></Form.Item><Form.Item label="默认外网 URL" name="externalURL" extra="第一版支持手动 URL；不会调用 UGREENlink 未公开 API。"><Input placeholder="https://nas.example.com" /></Form.Item></div><Divider /><div className="settings-section"><h2>界面</h2><Form.Item label="页面紧凑度" name="density"><Radio.Group options={[{ value: 'compact', label: '紧凑' }, { value: 'comfortable', label: '舒适' }]} /></Form.Item><Form.Item label="深色主题"><Space><Switch checked={dark} onChange={(checked) => { onThemeChange(checked); localStorage.setItem('cm-theme', checked ? 'dark' : 'light') }} /><Typography.Text type="secondary">{dark ? '深色' : '浅色'}</Typography.Text></Space></Form.Item></div><Form.Item wrapperCol={{ offset: 6, span: 14 }}><Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={saving}>保存设置</Button></Form.Item></Form></section>
}
