import { Tooltip } from 'antd'

const labels: Record<string, string> = {
  running: '运行中',
  stopped: '已停止',
  degraded: '异常',
  'not-running': '已停止',
  exited: '已停止',
  created: '已停止',
}

const tones: Record<string, 'running' | 'stopped' | 'warning'> = {
  running: 'running',
  stopped: 'stopped',
  degraded: 'warning',
  'not-running': 'stopped',
  exited: 'stopped',
  created: 'stopped',
}

export function StateBadge({ state }: { state: string }) {
  const label = labels[state] ?? '异常'
  return (
    <Tooltip title={label}>
      <span className="state-dot" role="img" aria-label={label}>
        <span className={`state-dot__circle state-dot__circle--${tones[state] ?? 'warning'}`} />
      </span>
    </Tooltip>
  )
}
