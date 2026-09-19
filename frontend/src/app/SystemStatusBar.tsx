import { ReloadOutlined } from '@ant-design/icons'
import { Button, Skeleton, Tooltip } from 'antd'
import { api } from '../api/client'
import { useResource } from '../hooks/useResource'
import { formatBytes, formatPercent } from '../utils/format'

export function SystemStatusBar() {
  const { data, error, loading, refresh } = useResource(api.overview, [])
  return (
    <header className="system-bar">
      {loading && !data ? <Skeleton.Input active size="small" /> : (
        <div className="system-stats">
          <span>CPU <strong>{formatPercent(data?.cpuPercent)}</strong></span><i />
          <span>RAM <strong>{formatBytes(data?.memoryUsed)} / {formatBytes(data?.memoryTotal)}</strong></span><i />
          <span>Compose <strong>{data?.projects ?? '—'}</strong></span><i />
          <span>容器 <strong>{data?.containersRunning ?? '—'} / {data?.containersTotal ?? '—'}</strong></span><i />
          <span>镜像 <strong>{data?.images ?? '—'}</strong></span><i />
          <span>可清理 <strong>{formatBytes(data?.reclaimableBytes)}</strong></span>
        </div>
      )}
      <div className="system-health">
        <span className={`health-dot ${error || !data?.dockerAvailable ? 'health-dot--error' : ''}`} />
        <span>{error || !data?.dockerAvailable ? 'Docker 未连接' : '系统正常'}</span>
        <Tooltip title="刷新"><Button aria-label="刷新系统状态" type="text" size="small" icon={<ReloadOutlined />} onClick={() => void refresh()} /></Tooltip>
      </div>
    </header>
  )
}

