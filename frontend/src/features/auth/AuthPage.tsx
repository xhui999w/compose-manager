import { useState } from 'react'
import { Alert, Button, Form, Input, Typography } from 'antd'
import { LockOutlined, SafetyCertificateOutlined, UserOutlined } from '@ant-design/icons'
import { APIError, api } from '../../api/client'
import type { AuthStatus } from '../../types'

type AuthValues = {
  setupToken?: string
  username: string
  password: string
  confirmPassword?: string
}

const errorMessages: Record<string, string> = {
  INVALID_SETUP_TOKEN: '初始化密钥不正确，请从容器日志中复制最新密钥。',
  ALREADY_INITIALIZED: '管理员账号已经创建，请刷新后登录。',
  INVALID_CREDENTIALS: '用户名或密码不正确。',
  TOO_MANY_ATTEMPTS: '登录尝试次数过多，请稍后再试。',
  ORIGIN_REJECTED: '请求来源不受信任，请使用面板本身的访问地址。',
}

export function AuthPage({ setupRequired, onAuthenticated }: { setupRequired: boolean; onAuthenticated: (status: AuthStatus) => void }) {
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const submit = async (values: AuthValues) => {
    setSubmitting(true)
    setError('')
    try {
      const status = setupRequired
        ? await api.setup(values.setupToken ?? '', values.username, values.password)
        : await api.login(values.username, values.password)
      onAuthenticated(status)
    } catch (reason) {
      if (reason instanceof APIError) setError(errorMessages[reason.code] ?? reason.message)
      else setError(reason instanceof Error ? reason.message : '请求失败，请稍后重试。')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="auth-page">
      <section className="auth-panel" aria-labelledby="auth-title">
        <div className="auth-brand"><span><SafetyCertificateOutlined /></span><div><strong>Compose Manager</strong><small>安全访问 Docker Compose</small></div></div>
        <div className="auth-heading">
          <Typography.Title id="auth-title" level={2}>{setupRequired ? '创建管理员账号' : '登录管理面板'}</Typography.Title>
          <Typography.Paragraph>{setupRequired ? '首次启动需要完成一次安全初始化。此实例仅允许创建一个管理员账号。' : '登录后才能访问 Docker Socket、Compose 文件和管理操作。'}</Typography.Paragraph>
        </div>
        {setupRequired ? <Alert className="auth-alert" type="info" showIcon title="初始化密钥在哪里？" description={<>在 NAS 中运行 <code>docker logs compose-manager</code>，查找 <code>setupToken</code>。创建账号后该密钥立即失效。</>} /> : null}
        {error ? <Alert className="auth-alert" type="error" showIcon title={error} /> : null}
        <Form<AuthValues> layout="vertical" requiredMark={false} onFinish={(values) => void submit(values)}>
          {setupRequired ? <Form.Item label="初始化密钥" name="setupToken" rules={[{ required: true, message: '请输入初始化密钥' }]}><Input.Password prefix={<SafetyCertificateOutlined />} autoComplete="one-time-code" placeholder="从容器日志复制" /></Form.Item> : null}
          <Form.Item label="用户名" name="username" rules={[{ required: true, message: '请输入用户名' }, { min: 3, max: 32, message: '用户名长度为 3–32 个字符' }, { pattern: /^[A-Za-z0-9_.-]+$/, message: '仅支持字母、数字、点、下划线和连字符' }]}><Input prefix={<UserOutlined />} autoComplete="username" autoFocus placeholder="admin" /></Form.Item>
          <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }, { min: 12, max: 128, message: '密码长度为 12–128 个字符' }]}><Input.Password prefix={<LockOutlined />} autoComplete={setupRequired ? 'new-password' : 'current-password'} placeholder={setupRequired ? '至少 12 个字符' : '输入密码'} /></Form.Item>
          {setupRequired ? <Form.Item label="确认密码" name="confirmPassword" dependencies={['password']} rules={[{ required: true, message: '请再次输入密码' }, ({ getFieldValue }) => ({ validator(_, value) { return !value || getFieldValue('password') === value ? Promise.resolve() : Promise.reject(new Error('两次输入的密码不一致')) } })]}><Input.Password prefix={<LockOutlined />} autoComplete="new-password" placeholder="再次输入密码" /></Form.Item> : null}
          <Button className="auth-submit" type="primary" htmlType="submit" loading={submitting}>{setupRequired ? '创建账号并登录' : '登录'}</Button>
        </Form>
        <p className="auth-footnote">建议仅在可信局域网使用；通过 HTTPS 反向代理访问时请设置 <code>CM_SECURE_COOKIE=true</code>。</p>
      </section>
    </main>
  )
}
