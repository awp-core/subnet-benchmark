package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
)

// ListWorkers returns all miners, ordered by most recently active first.
func (s *Store) ListWorkers(ctx context.Context) ([]model.Worker, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT address, suspended_until, epoch_violations, last_poll_at, created_at
		FROM workers ORDER BY last_poll_at DESC NULLS LAST`)
	if err != nil {
		return nil, fmt.Errorf("list miners: %w", err)
	}
	defer rows.Close()

	var result []model.Worker
	for rows.Next() {
		var w model.Worker
		if err := rows.Scan(&w.Address, &w.SuspendedUntil, &w.EpochViolations, &w.LastPollAt, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan miner: %w", err)
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

// ListAllQuestions returns questions with optional filters.
func (s *Store) ListAllQuestions(ctx context.Context, status string, limit int) ([]model.Question, error) {
	query := `
		SELECT question_id, bs_id, questioner, question, answer, status,
			share, score, pass_rate, benchmark, minhash, created_at, scored_at
		FROM questions`
	var args []any
	argIdx := 1
	if status != "" {
		query += fmt.Sprintf(" WHERE status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list all questions: %w", err)
	}
	defer rows.Close()

	var result []model.Question
	for rows.Next() {
		var q model.Question
		if err := rows.Scan(
			&q.QuestionID, &q.BSID, &q.Questioner, &q.Question, &q.Answer, &q.Status,
			&q.Share, &q.Score, &q.PassRate, &q.Benchmark, &q.MinHash, &q.CreatedAt, &q.ScoredAt,
		); err != nil {
			return nil, fmt.Errorf("scan question: %w", err)
		}
		result = append(result, q)
	}
	return result, rows.Err()
}

// ListAllAssignments returns assignments for a question.
func (s *Store) ListAllAssignments(ctx context.Context, questionID int64) ([]model.Assignment, error) {
	return s.ListAssignmentsByQuestion(ctx, questionID)
}

// ListEpochs returns all epochs, newest first.
func (s *Store) ListEpochs(ctx context.Context) ([]model.Epoch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT epoch_date, total_reward, total_scored, settled_at, merkle_root, published_at
		FROM epochs ORDER BY epoch_date DESC`)
	if err != nil {
		return nil, fmt.Errorf("list epochs: %w", err)
	}
	defer rows.Close()

	var result []model.Epoch
	for rows.Next() {
		var e model.Epoch
		if err := rows.Scan(&e.EpochDate, &e.TotalReward, &e.TotalScored, &e.SettledAt, &e.MerkleRoot, &e.PublishedAt); err != nil {
			return nil, fmt.Errorf("scan epoch: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// GetEpoch returns a single epoch, or nil if not found.
func (s *Store) GetEpoch(ctx context.Context, epochDate time.Time) (*model.Epoch, error) {
	var e model.Epoch
	err := s.db.QueryRowContext(ctx, `
		SELECT epoch_date, total_reward, total_scored, settled_at, merkle_root, published_at
		FROM epochs WHERE epoch_date = $1`, epochDate,
	).Scan(&e.EpochDate, &e.TotalReward, &e.TotalScored, &e.SettledAt, &e.MerkleRoot, &e.PublishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get epoch: %w", err)
	}
	return &e, nil
}
