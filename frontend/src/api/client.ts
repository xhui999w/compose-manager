import type { ComposeFile, ComposeVersion, Container, ImageReference, Project, SessionState, SystemInfo, UpdateRecord } from '../types'

type Envelope<T> = { data: T; warning?: string | null }
type APIErrorPayload = { error?: { code?: string; message?: string } }

export class APIError extends Error {
  constructor(public readonly code: string, message: string, public readonly status: number) {
    super(message)
  }
}

// 会话在其它标签页过期、或服务端重启后更换了签名密钥时，任意接口都会返回 401。
// 由 App 注册回调统一退回登录页，避免每个页面各写一遍跳转逻辑。
let unauthorizedHandler: (() => void) | null = null

export function setUnauthorizedHandler(handler: (() => void) | null) {
  unauthorizedHandler = handler
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    // 会话走 HttpOnly Cookie，必须显式携带凭证（跨源部署时也需要）。
    credentials: 'same-origin',
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as APIErrorPayload
    if (response.status === 401) unauthorizedHandler?.()
    throw new APIError(payload.error?.code ?? 'REQUEST_FAILED', payload.error?.message ?? response.statusText, response.status)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const api = {
  session: async () => (await request<Envelope<SessionState>>('/auth/session')).data,
  login: async (username: string, password: string) => (await request<Envelope<{ username: string; expiresAt: string }>>('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) })).data,
  logout: () => request<{ ok: boolean }>('/auth/logout', { method: 'POST' }),
  overview: async () => (await request<Envelope<SystemInfo>>('/overview')).data,
  projects: async () => (await request<Envelope<Project[]>>('/compose/projects')).data,
  projectAction: (key: string, action: string, service = '') => request<{ output: string }>(`/compose/projects/${encodeURIComponent(key)}/actions`, { method: 'POST', body: JSON.stringify({ action, service }) }),
  logs: (key: string, service = '') => request<{ logs: string }>(`/compose/projects/${encodeURIComponent(key)}/logs?tail=500&service=${encodeURIComponent(service)}`),
  file: async (key: string) => (await request<Envelope<ComposeFile>>(`/compose/projects/${encodeURIComponent(key)}/file`)).data,
  validateFile: (key: string, content: string) => request<{ valid: true; output: string; diff: string }>(`/compose/projects/${encodeURIComponent(key)}/file/validate`, { method: 'POST', body: JSON.stringify({ content }) }),
  saveFile: (key: string, content: string, baseSha: string, apply: boolean) => request(`/compose/projects/${encodeURIComponent(key)}/file`, { method: 'PUT', body: JSON.stringify({ content, baseSha, apply }) }),
  versions: async (key: string) => (await request<Envelope<ComposeVersion[]>>(`/compose/projects/${encodeURIComponent(key)}/versions`)).data,
  restore: (key: string, id: number, baseSha: string, apply: boolean) => request(`/compose/projects/${encodeURIComponent(key)}/versions/${id}/restore`, { method: 'POST', body: JSON.stringify({ baseSha, apply }) }),
  containers: async () => (await request<Envelope<Container[]>>('/containers')).data,
  containerAction: (id: string, action: string) => request(`/containers/${encodeURIComponent(id)}/actions`, { method: 'POST', body: JSON.stringify({ action }) }),
  inspectContainer: async (id: string) => (await request<Envelope<Record<string, unknown>>>(`/containers/${encodeURIComponent(id)}/inspect`)).data,
  containerLogs: (id: string) => request<{ logs: string }>(`/containers/${encodeURIComponent(id)}/logs?tail=500`),
  images: async () => (await request<Envelope<ImageReference[]>>('/images')).data,
  checkImageUpdates: async () => (await request<Envelope<ImageReference[]>>('/images/check-updates', { method: 'POST' })).data,
  deleteImage: (id: string) => request(`/images/${encodeURIComponent(id)}?confirm=true`, { method: 'DELETE' }),
  updates: async () => (await request<Envelope<UpdateRecord[]>>('/updates')).data,
  runUpdate: (project: string, service = '') => request('/updates/run', { method: 'POST', body: JSON.stringify({ project, service }) }),
  settings: async () => (await request<Envelope<Record<string, unknown>>>('/settings')).data,
  putSettings: (values: Record<string, unknown>) => request('/settings', { method: 'PUT', body: JSON.stringify(values) }),
}
