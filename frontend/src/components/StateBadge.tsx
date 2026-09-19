import { Badge } from 'antd'

const labels: Record<string, string> = { running: '运行中', stopped: '已停止', degraded: '异常', 'not-running': '未运行', exited: '已停止', created: '未运行' }
const statuses: Record<string, 'success' | 'error' | 'warning' | 'default'> = { running: 'success', stopped: 'error', degraded: 'error', 'not-running': 'default', exited: 'default', created: 'default' }

export function StateBadge({ state }: { state: string }) {
  return <Badge status={statuses[state] ?? 'warning'} text={labels[state] ?? state} />
}

