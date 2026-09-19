export function formatBytes(value = 0) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  const amount = value / 1024 ** index
  return `${amount >= 10 || index === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[index]}`
}

export function formatPercent(value = 0) { return `${value.toFixed(value < 1 ? 1 : 0)}%` }

export function formatDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
}

export function shortDigest(value?: string) {
  if (!value) return '—'
  const cleaned = value.replace('sha256:', '')
  return cleaned.length > 14 ? `sha256:${cleaned.slice(0, 12)}…` : value
}

