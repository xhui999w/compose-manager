import { useCallback, useEffect, useMemo, useState } from 'react'
import { CheckCircleOutlined, ClockCircleOutlined, CloseCircleOutlined, LoadingOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Progress, Space, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import type { UpdateTask } from '../../types'
import { formatDate } from '../../utils/format'

const stageLabels: Record<UpdateTask['stage'], string> = {
  queued: '等待开始',
  pulling: '下载镜像',
  applying: '重建容器',
  checking: '检查状态',
  completed: '更新完成',
  failed: '更新失败',
}

const statusColors: Record<UpdateTask['status'], string> = {
  queued: 'default',
  running: 'processing',
  success: 'success',
  failed: 'error',
}

function taskIcon(status: UpdateTask['status']) {
  if (status === 'running') return <LoadingOutlined spin />
  if (status === 'success') return <CheckCircleOutlined />
  if (status === 'failed') return <CloseCircleOutlined />
  return <ClockCircleOutlined />
}

export function UpdateProgressPage() {
  const [tasks, setTasks] = useState<UpdateTask[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const refresh = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      setTasks(await api.updateTasks())
      setError('')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '更新进度读取失败')
    } finally {
      if (!silent) setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(true), 1500)
    return () => window.clearInterval(timer)
  }, [refresh])

  const activeCount = useMemo(() => tasks.filter((task) => task.status === 'queued' || task.status === 'running').length, [tasks])
  const columns: ColumnsType<UpdateTask> = [
    { title: '状态', dataIndex: 'status', width: 92, render: (value: UpdateTask['status'], task) => <Tag icon={taskIcon(value)} color={statusColors[value]}>{stageLabels[task.stage]}</Tag> },
    { title: 'Compose 项目', dataIndex: 'project', width: 180, render: (value) => <strong>{value}</strong> },
    { title: '服务', dataIndex: 'service', width: 150, render: (value) => value || '全部服务' },
    {
      title: '执行进度', dataIndex: 'progress', width: 300,
      render: (value: number, task) => <div className="update-progress-cell"><Progress percent={value} size="small" status={task.status === 'failed' ? 'exception' : task.status === 'success' ? 'success' : 'active'} /><Typography.Text type="secondary">{task.message}</Typography.Text></div>,
    },
    { title: '开始时间', dataIndex: 'createdAt', width: 140, render: formatDate },
    { title: '最后变化', dataIndex: 'updatedAt', width: 140, render: formatDate },
    { title: '错误', dataIndex: 'error', ellipsis: true, render: (value) => value ? <Typography.Text type="danger">{value}</Typography.Text> : '—' },
  ]

  return (
    <section className="page update-progress-page">
      <PageHeader
        title="更新进度"
        description="实时查看镜像下载、容器重建和状态检查；页面每 1.5 秒自动刷新。"
        action={<Space><Tag color={activeCount ? 'processing' : 'default'}>{activeCount ? `${activeCount} 个任务进行中` : '当前无进行中任务'}</Tag><Button icon={<ReloadOutlined />} loading={loading} onClick={() => void refresh()}>刷新</Button></Space>}
      />
      {error ? <Alert className="inline-alert" type="warning" showIcon title="无法读取更新进度" description={error} /> : null}
      <Table<UpdateTask>
        className="dense-table"
        rowKey="id"
        size="small"
        columns={columns}
        dataSource={tasks}
        loading={loading}
        scroll={{ x: 1100 }}
        pagination={{ pageSize: 20, showTotal: (total) => `共 ${total} 个任务` }}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无更新任务；从 Compose 页面点击更新后，会在这里显示实时进度。" /> }}
        expandable={{
          rowExpandable: (task) => task.output.length > 0,
          expandedRowRender: (task) => <pre className="update-output">{task.output.join('\n')}</pre>,
        }}
      />
    </section>
  )
}
