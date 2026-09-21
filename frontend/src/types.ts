export type Port = { ip?: string; privatePort: number; publicPort?: number; type: string }

export type Container = {
  id: string
  name: string
  image: string
  imageId: string
  state: string
  status: string
  project?: string
  service?: string
  cpuPercent: number
  memoryBytes: number
  memoryLimit: number
  createdAt: string
  ports: Port[]
  updateStatus: 'available' | 'current' | 'unknown'
}

export type Project = {
  key: string
  name: string
  status: 'running' | 'stopped' | 'degraded' | 'not-running'
  healthy: number
  total: number
  cpuPercent: number
  memoryBytes: number
  updateStatus: 'available' | 'current' | 'unknown'
  updateCount: number
  internalUrl?: string
  externalUrl?: string
  configFile?: string
  workingDir?: string
  discoverySource: string
  editable: boolean
  containers: Container[]
}

export type SystemInfo = {
  dockerAvailable: boolean
  dockerVersion?: string
  cpus: number
  cpuPercent: number
  memoryUsed: number
  memoryTotal: number
  containersRunning: number
  containersTotal: number
  images: number
  projects: number
  reclaimableBytes: number
}

export type ImageReference = {
  id: string
  repository: string
  tag: string
  digest?: string
  size: number
  createdAt: string
  runningReferences: string[]
  stoppedReferences: string[]
  composeReferences: string[]
  updateStatus: string
  category: string
  reclaimableBytes: number
}

export type UpdateRecord = {
  id: number
  project: string
  service: string
  oldImage: string
  oldDigest: string
  newImage: string
  newDigest: string
  status: string
  error?: string
  createdAt: string
}

export type UpdateTask = {
  id: string
  project: string
  service: string
  status: 'queued' | 'running' | 'success' | 'failed'
  stage: 'queued' | 'pulling' | 'applying' | 'checking' | 'completed' | 'failed'
  progress: number
  message: string
  output: string[]
  error?: string
  createdAt: string
  updatedAt: string
  finishedAt?: string
}

export type ComposeFile = { path: string; content: string; sha256: string }
export type ComposeVersion = { id: number; projectKey: string; filePath: string; sha256: string; createdAt: string }

export type AuthStatus = {
  setupRequired: boolean
  authenticated: boolean
  username?: string
  csrfToken?: string
}
