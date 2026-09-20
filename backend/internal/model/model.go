package model

import "time"

type Port struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"privatePort"`
	PublicPort  uint16 `json:"publicPort,omitempty"`
	Type        string `json:"type"`
}

type Container struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	ImageID     string            `json:"imageId"`
	State       string            `json:"state"`
	Status      string            `json:"status"`
	Project     string            `json:"project,omitempty"`
	Service     string            `json:"service,omitempty"`
	CPUPercent  float64           `json:"cpuPercent"`
	MemoryBytes uint64            `json:"memoryBytes"`
	MemoryLimit uint64            `json:"memoryLimit"`
	CreatedAt   time.Time         `json:"createdAt"`
	Ports       []Port            `json:"ports"`
	Labels      map[string]string `json:"-"`
}

type Project struct {
	Key             string      `json:"key"`
	Name            string      `json:"name"`
	Status          string      `json:"status"`
	Healthy         int         `json:"healthy"`
	Total           int         `json:"total"`
	CPUPercent      float64     `json:"cpuPercent"`
	MemoryBytes     uint64      `json:"memoryBytes"`
	UpdateStatus    string      `json:"updateStatus"`
	UpdateCount     int         `json:"updateCount"`
	InternalURL     string      `json:"internalUrl,omitempty"`
	ExternalURL     string      `json:"externalUrl,omitempty"`
	ConfigFile      string      `json:"configFile,omitempty"`
	WorkingDir      string      `json:"workingDir,omitempty"`
	DiscoverySource string      `json:"discoverySource"`
	Editable        bool        `json:"editable"`
	Containers      []Container `json:"containers"`
}

type SystemInfo struct {
	DockerAvailable bool    `json:"dockerAvailable"`
	DockerVersion   string  `json:"dockerVersion,omitempty"`
	CPUs            int     `json:"cpus"`
	CPUPercent      float64 `json:"cpuPercent"`
	MemoryUsed      uint64  `json:"memoryUsed"`
	MemoryTotal     uint64  `json:"memoryTotal"`
	ContainersRun   int     `json:"containersRunning"`
	ContainersTotal int     `json:"containersTotal"`
	Images          int     `json:"images"`
	Projects        int     `json:"projects"`
	Reclaimable     uint64  `json:"reclaimableBytes"`
}

type ImageReference struct {
	ID                string    `json:"id"`
	Repository        string    `json:"repository"`
	Tag               string    `json:"tag"`
	Digest            string    `json:"digest,omitempty"`
	Size              uint64    `json:"size"`
	CreatedAt         time.Time `json:"createdAt"`
	RunningReferences []string  `json:"runningReferences"`
	StoppedReferences []string  `json:"stoppedReferences"`
	ComposeReferences []string  `json:"composeReferences"`
	UpdateStatus      string    `json:"updateStatus"`
	Category          string    `json:"category"`
	Reclaimable       uint64    `json:"reclaimableBytes"`
}

type UpdateRecord struct {
	ID        int64     `json:"id"`
	Project   string    `json:"project"`
	Service   string    `json:"service"`
	OldImage  string    `json:"oldImage"`
	OldDigest string    `json:"oldDigest"`
	NewImage  string    `json:"newImage"`
	NewDigest string    `json:"newDigest"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type ComposeVersion struct {
	ID         int64     `json:"id"`
	ProjectKey string    `json:"projectKey"`
	FilePath   string    `json:"filePath"`
	SHA256     string    `json:"sha256"`
	BackupPath string    `json:"-"`
	CreatedAt  time.Time `json:"createdAt"`
}
