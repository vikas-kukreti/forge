package store

import (
	"context"

	"forge/internal/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LedgerStore struct {
	pool *pgxpool.Pool
}

func NewLedgerStore(pool *pgxpool.Pool) *LedgerStore {
	return &LedgerStore{pool: pool}
}

func (s *LedgerStore) GetLedger(ctx context.Context, userID string, limit int, before *string) ([]*types.LedgerEntry, error) {
	// Simple query for now, not handling before cursor correctly in this snippet, just LIMIT
	rows, err := s.pool.Query(ctx, `
		SELECT delta_microcredits, balance_after, reason, ref_type, created_at
		FROM credit_ledger
		WHERE user_id = $1
		ORDER BY id DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*types.LedgerEntry
	for rows.Next() {
		entry := &types.LedgerEntry{}
		if err := rows.Scan(&entry.DeltaMicrocredits, &entry.BalanceAfter, &entry.Reason, &entry.RefType, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
