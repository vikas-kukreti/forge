package store

import (
	"context"
	"errors"

	"forge/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProjectStore struct {
	pool *pgxpool.Pool
}

func NewProjectStore(pool *pgxpool.Pool) *ProjectStore {
	return &ProjectStore{pool: pool}
}

func (s *ProjectStore) CountUserProjects(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM projects WHERE user_id = $1 AND archived_at IS NULL", userID).Scan(&count)
	return count, err
}

func (s *ProjectStore) CreateProject(ctx context.Context, userID, name, template, previewID string) (*types.ProjectSummary, error) {
	p := &types.ProjectSummary{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO projects (user_id, name, template, preview_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, template, preview_id, runtime_state, created_at, updated_at
	`, userID, name, template, previewID).Scan(
		&p.ID, &p.Name, &p.Template, &p.PreviewID, &p.RuntimeState, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *ProjectStore) ListProjects(ctx context.Context, userID string, limit int) ([]*types.ProjectSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, template, preview_id, runtime_state, created_at, updated_at
		FROM projects
		WHERE user_id = $1 AND archived_at IS NULL
		ORDER BY updated_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*types.ProjectSummary
	for rows.Next() {
		p := &types.ProjectSummary{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Template, &p.PreviewID, &p.RuntimeState, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *ProjectStore) GetProjectFull(ctx context.Context, id string, domain string) (*types.ProjectFull, error) {
	p := &types.ProjectFull{}
	var memLimitMB, cpuMillicores, diskQuotaMB int
	var ownerID string
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, template, preview_id, runtime_state, default_model,
			mem_limit_mb, cpu_millicores, disk_quota_mb, created_at, last_active_at
		FROM projects
		WHERE id = $1 AND archived_at IS NULL
	`, id).Scan(
		&p.ID, &ownerID, &p.Name, &p.Template, &p.PreviewID, &p.RuntimeState, &p.DefaultModel,
		&memLimitMB, &cpuMillicores, &diskQuotaMB, &p.CreatedAt, &p.LastActiveAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	p.Limits = &types.ProjectLimits{
		MemLimitMB:    memLimitMB,
		CPUMillicores: cpuMillicores,
		DiskQuotaMB:   diskQuotaMB,
	}
	p.PreviewURL = "https://" + p.PreviewID + ".preview." + domain

	// Fetch publish info if exists
	pub := &types.PublishInfo{}
	err = s.pool.QueryRow(ctx, `
		SELECT slug, kind, status
		FROM publishes
		WHERE project_id = $1
	`, id).Scan(&pub.Slug, &pub.Kind, &pub.Status)
	if err == nil {
		pub.URL = "https://" + pub.Slug + ".apps." + domain
		p.Publish = pub
	}

	return p, nil
}

func (s *ProjectStore) GetProjectOwner(ctx context.Context, id string) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx, "SELECT user_id FROM projects WHERE id = $1 AND archived_at IS NULL", id).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return userID, nil
}
