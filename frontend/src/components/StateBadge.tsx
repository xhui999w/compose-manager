import { Badge, Tooltip } from 'antd'

const labels: Record<string, string> = {
  running: '运行中',
  stopped: '已停止',
  degraded: '异常',
  'not-running': '已停止',
  exited: '已停止',
  created: '已停止',
}

const statuses: Record<string, 'success' | 'error' | 'warning'> = {
  running: 'success',
  stopped: 'error',
  degraded: 'warning',
  'not-running': 'error',
  exited: 'error',
  created: 'error',
}

export function StateBadge({ state }: { state: string }) {
  const label = labels[state] ?? '异常'
  return (
    <Tooltip title={label}>
      <span className="state-dot" role="img" aria-label={label}>
        <Badge status={statuses[state] ?? 'warning'} />
      </span>
    </Tooltip>
  )
}
