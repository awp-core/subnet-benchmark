package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
)

func (s *Store) CreateWorker(ctx context.Context, address string) (*model.Worker, error) {
	var w model.Worker
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO workers (address) VALUES ($1)
		ON CONFLICT (address) DO NOTHING
		RETURNING address, suspended_until, epoch_violations, last_poll_at, created_at`,
		address,
	).Scan(&w.Address, &w.SuspendedUntil, &w.EpochViolations, &w.LastPollAt, &w.CreatedAt)
	if err == sql.ErrNoRows {
		// Lost the race — another request already created this worker.
		return s.GetWorker(ctx, address)
	}
	if err != nil {
		return nil, fmt.Errorf("create worker: %w", err)
	}
	return &w, nil
}

func (s *Store) GetWorker(ctx context.Context, address string) (*model.Worker, error) {
	var w model.Worker
	err := s.db.QueryRowContext(ctx, `
		SELECT address, suspended_until, epoch_violations, last_poll_at, created_at
		FROM workers WHERE address = $1`, address,
	).Scan(&w.Address, &w.SuspendedUntil, &w.EpochViolations, &w.LastPollAt, &w.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get miner: %w", err)
	}
	return &w, nil
}

func (s *Store) UpdateWorkerLastPollAt(ctx context.Context, address string, t time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workers SET last_poll_at = $1 WHERE address = $2`, t, address)
	if err != nil {
		return fmt.Errorf("update last_poll_at: %w", err)
	}
	return nil
}

func (s *Store) SuspendWorker(ctx context.Context, address string, until time.Time, violations int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE workers SET suspended_until = $1, epoch_violations = $2
		WHERE address = $3`, until, violations, address)
	if err != nil {
		return fmt.Errorf("suspend miner: %w", err)
	}
	return nil
}

func (s *Store) UnsuspendWorker(ctx context.Context, address string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE workers SET suspended_until = NULL WHERE address = $1`, address)
	if err != nil {
		return fmt.Errorf("unsuspend miner: %w", err)
	}
	return nil
}
