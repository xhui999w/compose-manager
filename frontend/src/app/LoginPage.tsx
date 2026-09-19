import { useState } from 'react'
import { ClusterOutlined, LockOutlined, UserOutlined } from '@ant-design/icons'
import { Alert, Button, Form, Input } from 'antd'
import { APIError, api } from '../api/client'

type LoginValues = { username: string; password: string }

export function LoginPage({ onSuccess }: { onSuccess: (username: string) => void }) {
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (values: LoginValues) => {
    setSubmitting(true)
    setError(null)
    try {
      const session = await api.login(values.username, values.password)
      onSuccess(session.username)
    } catch (cause) {
      setError(cause instanceof APIError ? cause.message : '登录失败，请检查网络后重试')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-card">
        <div className="login-brand">
          <div className="brand-mark"><ClusterOutlined /></div>
          <div className="brand-copy"><strong>Compose Manager</strong><span>更简单的 Compose 管理</span></div>
        </div>
        <p className="login-hint">该面板可直接操作宿主机 Docker，请先登录。</p>
        {error ? <Alert className="inline-alert" type="error" showIcon message={error} /> : null}
        <Form<LoginValues>
          layout="vertical"
          requiredMark={false}
          initialValues={{ username: 'admin' }}
          onFinish={(values) => void submit(values)}
        >
          <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input prefix={<UserOutlined />} autoComplete="username" autoFocus placeholder="admin" />
          </Form.Item>
          <Form.Item name="password" label="口令" rules={[{ required: true, message: '请输入口令' }]}>
            <Input.Password prefix={<LockOutlined />} autoComplete="current-password" placeholder="请输入口令" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={submitting}>登录</Button>
        </Form>
      </div>
    </div>
  )
}
