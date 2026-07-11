package store

import (
	"context"
	"errors"

	"forge/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) *UserStore {
	return &UserStore{pool: pool}
}

func (s *UserStore) CreateUser(ctx context.Context, email, passwordHash, displayName string, isAdmin bool) (*types.User, error) {
	u := &types.User{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, display_name, is_admin)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email, display_name, is_admin, balance_microcredits, status
	`, email, passwordHash, displayName, isAdmin).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.IsAdmin, &u.BalanceMicrocredits, &u.Status,
	)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *UserStore) GetUserByEmail(ctx context.Context, email string) (*types.User, string, error) {
	u := &types.User{}
	var pwdHash string
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, display_name, is_admin, balance_microcredits, status
		FROM users
		WHERE email = $1
	`, email).Scan(
		&u.ID, &u.Email, &pwdHash, &u.DisplayName, &u.IsAdmin, &u.BalanceMicrocredits, &u.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", err
	}
	return u, pwdHash, nil
}

func (s *UserStore) GetUserByID(ctx context.Context, id string) (*types.User, error) {
	u := &types.User{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, display_name, is_admin, balance_microcredits, status
		FROM users
		WHERE id = $1
	`, id).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.IsAdmin, &u.BalanceMicrocredits, &u.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

func (s *UserStore) SuspendUser(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, "UPDATE users SET status = 'suspended' WHERE id = $1", id)
	return err
}

func (s *UserStore) ListUsers(ctx context.Context, limit int, offset int) ([]*types.AdminUserRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, email, display_name, is_admin, status, balance_microcredits
		FROM users
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*types.AdminUserRow
	for rows.Next() {
		u := &types.AdminUserRow{}
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsAdmin, &u.Status, &u.BalanceMicrocredits); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
