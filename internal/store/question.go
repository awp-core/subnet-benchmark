package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
)

func (s *Store) CreateQuestion(ctx context.Context, q model.Question) (*model.Question, error) {
	var out model.Question
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO questions (bs_id, questioner, question, answer, minhash)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING question_id, bs_id, questioner, question, answer, status,
			share, score, pass_rate, benchmark, minhash, created_at, scored_at`,
		q.BSID, q.Questioner, q.Question, q.Answer, q.MinHash,
	).Scan(
		&out.QuestionID, &out.BSID, &out.Questioner, &out.Question, &out.Answer, &out.Status,
		&out.Share, &out.Score, &out.PassRate, &out.Benchmark, &out.MinHash, &out.CreatedAt, &out.ScoredAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create question: %w", err)
	}
	return &out, nil
}

func (s *Store) GetQuestion(ctx context.Context, id int64) (*model.Question, error) {
	var q model.Question
	err := s.db.QueryRowContext(ctx, `
		SELECT question_id, bs_id, questioner, question, answer, status,
			share, score, pass_rate, benchmark, minhash, created_at, scored_at
		FROM questions WHERE question_id = $1`, id,
	).Scan(
		&q.QuestionID, &q.BSID, &q.Questioner, &q.Question, &q.Answer, &q.Status,
		&q.Share, &q.Score, &q.PassRate, &q.Benchmark, &q.MinHash, &q.CreatedAt, &q.ScoredAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get question: %w", err)
	}
	return &q, nil
}

// ListQuestionMinHashes returns MinHash signatures for questions in the given BenchmarkSet with score >= minScore.
func (s *Store) ListQuestionMinHashes(ctx context.Context, bsID string, minScore int) ([][]byte, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT minhash FROM questions
		WHERE bs_id = $1 AND score >= $2 AND minhash IS NOT NULL`,
		bsID, minScore)
	if err != nil {
		return nil, fmt.Errorf("list question minhashes: %w", err)
	}
	defer rows.Close()

	var result [][]byte
	for rows.Next() {
		var h []byte
		if err := rows.Scan(&h); err != nil {
			return nil, fmt.Errorf("scan minhash: %w", err)
		}
		result = append(result, h)
	}
	return result, rows.Err()
}

// GetLastQuestionTime returns the most recent question submission time for the given miner.
func (s *Store) GetLastQuestionTime(ctx context.Context, questioner string) (*time.Time, error) {
	var t time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT created_at FROM questions WHERE questioner = $1
		ORDER BY created_at DESC LIMIT 1`, questioner).Scan(&t)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get last question time: %w", err)
	}
	return &t, nil
}

// ScoreQuestion updates the score, share, pass_rate, and status of a question.
// Accepts both 'submitted' and 'answering' as valid pre-score states.
// Returns true if the question was updated, false if already scored.
func (s *Store) ScoreQuestion(ctx context.Context, id int64, score int, share float64, passRate float64, scoredAt time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE questions SET status = 'scored', score = $1, share = $2, pass_rate = $3, scored_at = $4
		WHERE question_id = $5 AND status = 'submitted'`, score, share, passRate, scoredAt, id)
	if err != nil {
		return false, fmt.Errorf("score question: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// PickQuestionForWorker selects a random submitted question (not by the worker)
// from the oldest 100 that still need answers, locks it with FOR UPDATE to
// prevent concurrent over-assignment.
// A worker is excluded from a question if they have a non-timed-out assignment
// (claimed/replied/scored), or if they already timed out on it twice.
func (s *Store) PickQuestionForWorker(ctx context.Context, worker string, requiredAnswers int) (*model.Question, error) {
	var q model.Question
	err := s.db.QueryRowContext(ctx, `
		WITH candidates AS (
			SELECT q.question_id
			FROM questions q
			WHERE q.status = 'submitted' AND q.questioner != $1
			AND NOT EXISTS (
				SELECT 1 FROM assignments a
				WHERE a.question_id = q.question_id AND a.worker = $1
				AND a.status IN ('claimed', 'replied', 'scored')
			)
			AND (SELECT count(*) FROM assignments a3
				WHERE a3.question_id = q.question_id AND a3.worker = $1
				AND a3.status = 'timed-out') < 2
			AND (SELECT count(*) FROM assignments a2
				WHERE a2.question_id = q.question_id AND a2.status IN ('claimed', 'replied')) < $2
			ORDER BY q.created_at LIMIT 100
		)
		SELECT q.question_id, q.bs_id, q.questioner, q.question, q.answer, q.status,
			q.share, q.score, q.pass_rate, q.benchmark, q.minhash, q.created_at, q.scored_at
		FROM candidates c
		JOIN questions q ON q.question_id = c.question_id
		ORDER BY random()
		FOR UPDATE OF q SKIP LOCKED
		LIMIT 1`, worker, requiredAnswers,
	).Scan(
		&q.QuestionID, &q.BSID, &q.Questioner, &q.Question, &q.Answer, &q.Status,
		&q.Share, &q.Score, &q.PassRate, &q.Benchmark, &q.MinHash, &q.CreatedAt, &q.ScoredAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pick question for miner: %w", err)
	}
	return &q, nil
}

// QuestionFilter holds optional filters for listing questions.
type QuestionFilter struct {
	Questioner string
	Status     string     // Optional: filter by status
	From       *time.Time // Optional: created_at >= from
	To         *time.Time // Optional: created_at < to
}

// ListQuestionsByFilter returns questions matching the given filter.
func (s *Store) ListQuestionsByFilter(ctx context.Context, f QuestionFilter) ([]model.Question, error) {
	query := `
		SELECT question_id, bs_id, questioner, question, answer, status,
			share, score, pass_rate, benchmark, minhash, created_at, scored_at
		FROM questions WHERE questioner = $1`
	args := []any{f.Questioner}
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
		return nil, fmt.Errorf("list questions by filter: %w", err)
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

// IncrementBSTotalQuestions increments the total_questions counter of a BenchmarkSet.
func (s *Store) IncrementBSTotalQuestions(ctx context.Context, bsID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE benchmark_sets SET total_questions = total_questions + 1 WHERE set_id = $1`, bsID)
	if err != nil {
		return fmt.Errorf("increment bs total_questions: %w", err)
	}
	return nil
}
