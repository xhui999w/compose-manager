import type { AuthStatus, ComposeFile, ComposeVersion, Container, ImageReference, Project, SystemInfo, UpdateRecord } from '../types'

type Envelope<T> = { data: T; warning?: string | null }
type APIErrorPayload = { error?: { code?: string; message?: string } }

export class APIError extends Error {
  constructor(public readonly code: string, message: string, public readonly status: number) {
    super(message)
  }
}

export const AUTH_REQUIRED_EVENT = 'compose-manager:auth-required'

let csrfToken = ''

function applyAuthStatus(status: AuthStatus) {
  csrfToken = status.authenticated ? status.csrfToken ?? '' : ''
  return status
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	const method = (init?.method ?? 'GET').toUpperCase()
	const headers = new Headers(init?.headers)
	headers.set('Content-Type', 'application/json')
	if (!['GET', 'HEAD', 'OPTIONS'].includes(method) && csrfToken) headers.set('X-CSRF-Token', csrfToken)
  const response = await fetch(`/api/v1${path}`, {
    ...init,
		credentials: 'same-origin',
		headers,
  })
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as APIErrorPayload
		if (response.status === 401 && !path.startsWith('/auth/')) {
			csrfToken = ''
			window.dispatchEvent(new Event(AUTH_REQUIRED_EVENT))
		}
    throw new APIError(payload.error?.code ?? 'REQUEST_FAILED', payload.error?.message ?? response.statusText, response.status)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const api = {
	authStatus: async () => applyAuthStatus((await request<Envelope<AuthStatus>>('/auth/status')).data),
	setup: async (setupToken: string, username: string, password: string) => applyAuthStatus((await request<Envelope<AuthStatus>>('/auth/setup', { method: 'POST', body: JSON.stringify({ setupToken, username, password }) })).data),
	login: async (username: string, password: string) => applyAuthStatus((await request<Envelope<AuthStatus>>('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) })).data),
	logout: async () => { await request<void>('/auth/logout', { method: 'POST' }); csrfToken = '' },
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
  containerLogs: (id: string) => request<{ logs: string }>(`/containers/${encodeURIComponent(id)}/logs?tail=500`),
  images: async () => (await request<Envelope<ImageReference[]>>('/images')).data,
  deleteImage: (id: string) => request(`/images/${encodeURIComponent(id)}?confirm=true`, { method: 'DELETE' }),
  updates: async () => (await request<Envelope<UpdateRecord[]>>('/updates')).data,
  runUpdate: (project: string, service = '') => request('/updates/run', { method: 'POST', body: JSON.stringify({ project, service }) }),
  settings: async () => (await request<Envelope<Record<string, unknown>>>('/settings')).data,
  putSettings: (values: Record<string, unknown>) => request('/settings', { method: 'PUT', body: JSON.stringify(values) }),
}
