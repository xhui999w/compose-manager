import { Alert, Button, Table, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { useResource } from '../../hooks/useResource'
import type { UpdateRecord } from '../../types'
import { formatDate, shortDigest } from '../../utils/format'

export function UpdatesPage() {
  const { data = [], error, loading, refresh } = useResource(api.updates, [])
  const columns: ColumnsType<UpdateRecord> = [
    { title: '时间', dataIndex: 'createdAt', width: 130, render: formatDate },
    { title: 'Compose 项目', dataIndex: 'project', width: 170, render: (value) => <strong>{value}</strong> },
    { title: '服务', dataIndex: 'service', width: 140, render: (value) => value || '全部服务' },
    { title: '原镜像', dataIndex: 'oldImage', width: 190, ellipsis: true, render: (value) => value || '—' },
    { title: '原 Digest', dataIndex: 'oldDigest', width: 160, render: shortDigest },
    { title: '新镜像', dataIndex: 'newImage', width: 190, ellipsis: true, render: (value) => value || '—' },
    { title: '新 Digest', dataIndex: 'newDigest', width: 160, render: shortDigest },
    { title: '结果', dataIndex: 'status', width: 92, render: (value) => <Tag color={value === 'success' ? 'green' : value === 'failed' ? 'red' : 'blue'}>{value === 'success' ? '成功' : value === 'failed' ? '失败' : '进行中'}</Tag> },
    { title: '错误信息', dataIndex: 'error', ellipsis: true, render: (value) => value ? <Typography.Text type="danger">{value}</Typography.Text> : '—' },
  ]
  return <section className="page"><PageHeader title="更新记录" description="记录每次更新的旧/新镜像、Digest、结果和错误，为后续回滚保留依据。" action={<Button icon={<ReloadOutlined />} onClick={() => void refresh()} loading={loading}>刷新</Button>} />{error ? <Alert className="inline-alert" type="warning" showIcon title="更新记录不可用" description={error.message} /> : null}<Table<UpdateRecord> className="dense-table" rowKey="id" size="small" columns={columns} dataSource={data} loading={loading} scroll={{ x: 1320 }} pagination={{ pageSize: 20, showTotal: (total) => `共 ${total} 条记录` }} /></section>
}

