package types

// NodeHeartbeat represents the periodic heartbeat sent by forge-noded
type NodeHeartbeat struct {
	Name         string        `json:"name"`
	InternalAddr string        `json:"internal_addr"`
	Caps         NodeCaps      `json:"caps"`
	Alloc        NodeCaps      `json:"alloc"`
	Sandboxes    []SandboxInfo `json:"sandboxes"`
	Apps         []AppInfo     `json:"apps"`
	Version      string        `json:"version"`
}

type NodeCaps struct {
	CPUMillicores int `json:"cpu_millicores"`
	MemMB         int `json:"mem_mb"`
	DiskMB        int `json:"disk_mb"`
}

type SandboxInfo struct {
	ProjectID string `json:"project_id"`
	State     string `json:"state"`
}

type AppInfo struct {
	PublishID string `json:"publish_id"`
	State     string `json:"state"`
}
