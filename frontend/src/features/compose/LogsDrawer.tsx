import { useEffect, useState } from 'react'
import { Button, Drawer, Select, Skeleton, Space, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { api } from '../../api/client'
import type { Project } from '../../types'

export function LogsDrawer({ project, open, onClose }: { project?: Project; open: boolean; onClose: () => void }) {
  const [service, setService] = useState('')
  const [logs, setLogs] = useState('')
  const [loading, setLoading] = useState(false)
  const load = async () => {
    if (!project) return
    setLoading(true)
    try { setLogs((await api.logs(project.key, service)).logs) }
    catch (reason) { setLogs(reason instanceof Error ? reason.message : '日志读取失败') }
    finally { setLoading(false) }
  }
  useEffect(() => { if (open) void load() }, [open, project, service])
  return (
    <Drawer width="min(840px, 90vw)" open={open} onClose={onClose} destroyOnHidden title={`${project?.name ?? ''} 日志`} extra={<Space><Select allowClear placeholder="全部服务" value={service || undefined} onChange={(value) => setService(value ?? '')} options={project?.containers.map((container) => ({ value: container.service || container.name, label: container.service || container.name }))} /><Button icon={<ReloadOutlined />} onClick={() => void load()}>刷新</Button></Space>}>
      {loading ? <Skeleton active /> : <pre className="log-view"><Typography.Text>{logs || '暂无日志'}</Typography.Text></pre>}
    </Drawer>
  )
}

