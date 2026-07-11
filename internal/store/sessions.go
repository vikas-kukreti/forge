package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionStore struct {
	pool *pgxpool.Pool
}

func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
	return &SessionStore{pool: pool}
}

func (s *SessionStore) CreateSession(ctx context.Context, userID string, tokenHash []byte, ip *string, userAgent *string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO auth_sessions (user_id, token_hash, ip, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, tokenHash, ip, userAgent, expiresAt)
	return err
}

func (s *SessionStore) GetSessionUser(ctx context.Context, tokenHash []byte) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx, `
		SELECT user_id
		FROM auth_sessions
		WHERE token_hash = $1 AND expires_at > now()
	`, tokenHash).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return userID, nil
}

func (s *SessionStore) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM auth_sessions WHERE token_hash = $1", tokenHash)
	return err
}
