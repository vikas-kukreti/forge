package credits

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Manager struct {
	pool *pgxpool.Pool
}

func NewManager(pool *pgxpool.Pool) *Manager {
	return &Manager{pool: pool}
}

// GrantCredits grants credits and logs the transaction.
func (m *Manager) GrantCredits(ctx context.Context, userID string, delta int64, reason string) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var balanceAfter int64
	err = tx.QueryRow(ctx, `
		UPDATE users
		SET balance_microcredits = balance_microcredits + $1
		WHERE id = $2
		RETURNING balance_microcredits
	`, delta, userID).Scan(&balanceAfter)
	if err != nil {
		return fmt.Errorf("failed to update user balance: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO credit_ledger (user_id, delta_microcredits, balance_after, reason)
		VALUES ($1, $2, $3, $4)
	`, userID, delta, balanceAfter, reason)
	if err != nil {
		return fmt.Errorf("failed to insert ledger entry: %w", err)
	}

	return tx.Commit(ctx)
}

// DebitCredits subtracts credits.
func (m *Manager) DebitCredits(ctx context.Context, userID string, delta int64, reason string, refType string, refID string) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var balanceAfter int64
	err = tx.QueryRow(ctx, `
		UPDATE users
		SET balance_microcredits = balance_microcredits - $1
		WHERE id = $2
		RETURNING balance_microcredits
	`, delta, userID).Scan(&balanceAfter)
	if err != nil {
		return fmt.Errorf("failed to update user balance: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO credit_ledger (user_id, delta_microcredits, balance_after, reason, ref_type, ref_id)
		VALUES ($1, -$2, $3, $4, $5, $6)
	`, userID, delta, balanceAfter, reason, refType, refID)
	if err != nil {
		return fmt.Errorf("failed to insert ledger entry: %w", err)
	}

	return tx.Commit(ctx)
}

// Reconcile checks if user balances match the sum of ledger deltas.
func (m *Manager) Reconcile(ctx context.Context) error {
	rows, err := m.pool.Query(ctx, `
		SELECT u.id, u.balance_microcredits, COALESCE(SUM(l.delta_microcredits), 0)
		FROM users u
		LEFT JOIN credit_ledger l ON u.id = l.user_id
		GROUP BY u.id, u.balance_microcredits
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	mismatches := 0
	for rows.Next() {
		var id string
		var balance, sum int64
		if err := rows.Scan(&id, &balance, &sum); err != nil {
			return err
		}
		if balance != sum {
			slog.Error("ledger mismatch", "user_id", id, "balance", balance, "sum", sum)
			mismatches++
		}
	}

	if mismatches > 0 {
		return fmt.Errorf("reconciler found %d mismatches", mismatches)
	}

	slog.Info("reconciler passed", "mismatches", 0)
	return nil
}
