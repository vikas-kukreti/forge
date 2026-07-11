package types

import "time"

type User struct {
	ID                  string `json:"id"`
	Email               string `json:"email"`
	DisplayName         string `json:"display_name"`
	IsAdmin             bool   `json:"is_admin"`
	BalanceMicrocredits int64  `json:"balance_microcredits"`
	Status              string `json:"-"`
}

type SignupRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type MeResponse struct {
	User *User `json:"user"`
}

type LedgerEntry struct {
	DeltaMicrocredits int64     `json:"delta_microcredits"`
	BalanceAfter      int64     `json:"balance_after"`
	Reason            string    `json:"reason"`
	RefType           *string   `json:"ref_type,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

type LedgerResponse struct {
	Entries    []*LedgerEntry `json:"entries"`
	NextCursor *string        `json:"next_cursor,omitempty"`
}

type ProjectSummary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Template      string    `json:"template"`
	PreviewID     string    `json:"preview_id"`
	RuntimeState  string    `json:"runtime_state"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ProjectsResponse struct {
	Projects []*ProjectSummary `json:"projects"`
}

type CreateProjectRequest struct {
	Name     string `json:"name"`
	Template string `json:"template"`
}

type ProjectLimits struct {
	MemLimitMB    int `json:"mem_limit_mb"`
	CPUMillicores int `json:"cpu_millicores"`
	DiskQuotaMB   int `json:"disk_quota_mb"`
}

type PublishInfo struct {
	Slug   string `json:"slug"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	URL    string `json:"url"`
}

type ProjectFull struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Template      string         `json:"template"`
	PreviewID     string         `json:"preview_id"`
	RuntimeState  string         `json:"runtime_state"`
	DefaultModel  *string        `json:"default_model"`
	Limits        *ProjectLimits `json:"limits"`
	PreviewURL    string         `json:"preview_url"`
	Publish       *PublishInfo   `json:"publish,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	LastActiveAt  time.Time      `json:"last_active_at"`
}

type UpdateProjectRequest struct {
	Name         *string `json:"name,omitempty"`
	DefaultModel *string `json:"default_model,omitempty"`
}

type AdminUserRow struct {
	ID                  string `json:"id"`
	Email               string `json:"email"`
	DisplayName         string `json:"display_name"`
	IsAdmin             bool   `json:"is_admin"`
	Status              string `json:"status"`
	BalanceMicrocredits int64  `json:"balance_microcredits"`
}

type AdminGrantRequest struct {
	Credits int64 `json:"credits"`
}
