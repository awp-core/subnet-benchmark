package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
)

func (s *Store) CreateAssignment(ctx context.Context, a model.Assignment) (*model.Assignment, error) {
	var out model.Assignment
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO assignments (question_id, worker, reply_ddl)
		VALUES ($1, $2, $3)
		RETURNING assignment_id, question_id, worker, status, reply_ddl,
			reply_valid, reply_answer, replied_at, share, score, created_at`,
		a.QuestionID, a.Worker, a.ReplyDDL,
	).Scan(
		&out.AssignmentID, &out.QuestionID, &out.Worker, &out.Status, &out.ReplyDDL,
		&out.ReplyValid, &out.ReplyAnswer, &out.RepliedAt, &out.Share, &out.Score, &out.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create assignment: %w", err)
	}
	return &out, nil
}

func (s *Store) GetAssignment(ctx context.Context, id int64) (*model.Assignment, error) {
	var a model.Assignment
	err := s.db.QueryRowContext(ctx, `
		SELECT assignment_id, question_id, worker, status, reply_ddl,
			reply_valid, reply_answer, replied_at, share, score, created_at
		FROM assignments WHERE assignment_id = $1`, id,
	).Scan(
		&a.AssignmentID, &a.QuestionID, &a.Worker, &a.Status, &a.ReplyDDL,
		&a.ReplyValid, &a.ReplyAnswer, &a.RepliedAt, &a.Share, &a.Score, &a.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get assignment: %w", err)
	}
	return &a, nil
}

func (s *Store) GetAssignmentByWorker(ctx context.Context, questionID int64, worker string) (*model.Assignment, error) {
	var a model.Assignment
	err := s.db.QueryRowContext(ctx, `
		SELECT assignment_id, question_id, worker, status, reply_ddl,
			reply_valid, reply_answer, replied_at, share, score, created_at
		FROM assignments WHERE question_id = $1 AND worker = $2`, questionID, worker,
	).Scan(
		&a.AssignmentID, &a.QuestionID, &a.Worker, &a.Status, &a.ReplyDDL,
		&a.ReplyValid, &a.ReplyAnswer, &a.RepliedAt, &a.Share, &a.Score, &a.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get assignment by miner: %w", err)
	}
	return &a, nil
}

// FindClaimedAssignment finds the miner's current claimed assignment (if any).
func (s *Store) FindClaimedAssignment(ctx context.Context, miner string) (*model.Assignment, error) {
	var a model.Assignment
	err := s.db.QueryRowContext(ctx, `
		SELECT assignment_id, question_id, worker, status, reply_ddl,
			reply_valid, reply_answer, replied_at, share, score, created_at
		FROM assignments
		WHERE worker = $1 AND status = 'claimed'
		ORDER BY created_at DESC LIMIT 1`, miner,
	).Scan(
		&a.AssignmentID, &a.QuestionID, &a.Worker, &a.Status, &a.ReplyDDL,
		&a.ReplyValid, &a.ReplyAnswer, &a.RepliedAt, &a.Share, &a.Score, &a.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find claimed assignment: %w", err)
	}
	return &a, nil
}

func (s *Store) ListAssignmentsByQuestion(ctx context.Context, questionID int64) ([]model.Assignment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT assignment_id, question_id, worker, status, reply_ddl,
			reply_valid, reply_answer, replied_at, share, score, created_at
		FROM assignments WHERE question_id = $1 ORDER BY created_at`, questionID)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()

	var result []model.Assignment
	for rows.Next() {
		var a model.Assignment
		if err := rows.Scan(
			&a.AssignmentID, &a.QuestionID, &a.Worker, &a.Status, &a.ReplyDDL,
			&a.ReplyValid, &a.ReplyAnswer, &a.RepliedAt, &a.Share, &a.Score, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan assignment: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// SetAssignmentReply updates the reply content and sets status to replied.
func (s *Store) SetAssignmentReply(ctx context.Context, id int64, valid bool, answer string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE assignments SET status = 'replied', reply_valid = $1, reply_answer = $2, replied_at = now()
		WHERE assignment_id = $3 AND status = 'claimed'`, valid, answer, id)
	if err != nil {
		return fmt.Errorf("set assignment reply: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("set assignment reply: not found or not claimed")
	}
	return nil
}

// SetAssignmentScore updates the score and status of an assignment.
func (s *Store) SetAssignmentScore(ctx context.Context, id int64, status string, score int, share float64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE assignments SET status = $1, score = $2, share = $3
		WHERE assignment_id = $4`, status, score, share, id)
	if err != nil {
		return fmt.Errorf("set assignment score: %w", err)
	}
	return nil
}

// AssignmentFilter holds optional filters for listing assignments.
type AssignmentFilter struct {
	Worker string
	Status string
	From   *time.Time
	To     *time.Time
}

func (s *Store) ListAssignmentsByFilter(ctx context.Context, f AssignmentFilter) ([]model.Assignment, error) {
	query := `
		SELECT assignment_id, question_id, worker, status, reply_ddl,
			reply_valid, reply_answer, replied_at, share, score, created_at
		FROM assignments WHERE worker = $1`
	args := []any{f.Worker}
	argIdx := 2

	if f.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, f.Status)
		argIdx++
	}
	if f.From != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *f.From)
		argIdx++
	}
	if f.To != nil {
		query += fmt.Sprintf(" AND created_at < $%d", argIdx)
		args = append(args, *f.To)
		argIdx++
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list assignments by filter: %w", err)
	}
	defer rows.Close()

	var result []model.Assignment
	for rows.Next() {
		var a model.Assignment
		if err := rows.Scan(
			&a.AssignmentID, &a.QuestionID, &a.Worker, &a.Status, &a.ReplyDDL,
			&a.ReplyValid, &a.ReplyAnswer, &a.RepliedAt, &a.Share, &a.Score, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan assignment: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// CountActiveAssignments returns the number of claimed + replied assignments for a question.
// Call within a transaction after FOR UPDATE to get an accurate count.
func (s *Store) CountActiveAssignments(ctx context.Context, questionID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM assignments
		WHERE question_id = $1 AND status IN ('claimed', 'replied')`, questionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active assignments: %w", err)
	}
	return count, nil
}

// TimeoutAssignment atomically marks a claimed assignment as timed-out.
// Returns true if the assignment was updated, false if it was no longer claimed (e.g. reply submitted).
func (s *Store) TimeoutAssignment(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE assignments SET status = 'timed-out', score = 0, share = 0
		WHERE assignment_id = $1 AND status = 'claimed'`, id)
	if err != nil {
		return false, fmt.Errorf("timeout assignment: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

