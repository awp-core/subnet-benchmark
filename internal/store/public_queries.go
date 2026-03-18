package store

import (
	"context"
	"fmt"
)

// ProtocolStats holds aggregate stats for the landing page.
type ProtocolStats struct {
	WorkerCount    int   `json:"worker_count"`
	QuestionCount int   `json:"question_count"`
	ScoredCount   int   `json:"scored_count"`
	EpochCount    int   `json:"epoch_count"`
	TotalReward   int64 `json:"total_reward"`
}

func (s *Store) GetProtocolStats(ctx context.Context) (*ProtocolStats, error) {
	var st ProtocolStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT count(*) FROM workers),
			(SELECT count(*) FROM questions),
			(SELECT count(*) FROM questions WHERE status = 'scored'),
			(SELECT count(*) FROM epochs),
			COALESCE((SELECT sum(total_reward) FROM epochs), 0)
	`).Scan(&st.WorkerCount, &st.QuestionCount, &st.ScoredCount, &st.EpochCount, &st.TotalReward)
	if err != nil {
		return nil, fmt.Errorf("get protocol stats: %w", err)
	}
	return &st, nil
}

type LeaderboardEntry struct {
	Address       string  `json:"address"`
	QuestionCount int     `json:"question_count"`
	AnswerCount   int     `json:"answer_count"`
	AvgScore      float64 `json:"avg_score"`
	TotalReward   float64 `json:"total_reward"`
}

func (s *Store) GetLeaderboard(ctx context.Context, limit int) ([]LeaderboardEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			m.address,
			COALESCE(q.cnt, 0) AS question_count,
			COALESCE(a.cnt, 0) AS answer_count,
			COALESCE(
				(COALESCE(q.avg_score, 0) + COALESCE(a.avg_score, 0)) /
				NULLIF((CASE WHEN q.avg_score IS NOT NULL THEN 1 ELSE 0 END +
				        CASE WHEN a.avg_score IS NOT NULL THEN 1 ELSE 0 END), 0),
			0) AS avg_score,
			COALESCE(r.total, 0) AS total_reward
		FROM workers m
		LEFT JOIN (
			SELECT questioner, count(*) AS cnt, avg(score) AS avg_score
			FROM questions WHERE status = 'scored'
			GROUP BY questioner
		) q ON q.questioner = m.address
		LEFT JOIN (
			SELECT worker, count(*) AS cnt, avg(score) AS avg_score
			FROM assignments WHERE status = 'scored'
			GROUP BY worker
		) a ON a.worker = m.address
		LEFT JOIN (
			SELECT worker_address, sum(final_reward) AS total
			FROM worker_epoch_rewards
			GROUP BY worker_address
		) r ON r.worker_address = m.address
		ORDER BY total_reward DESC, avg_score DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("get leaderboard: %w", err)
	}
	defer rows.Close()

	var result []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(&e.Address, &e.QuestionCount, &e.AnswerCount, &e.AvgScore, &e.TotalReward); err != nil {
			return nil, fmt.Errorf("scan leaderboard: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

type WorkerAggregateStats struct {
	TotalQuestions    int   `json:"total_questions"`
	ScoredQuestions   int   `json:"scored_questions"`
	TotalAssignments  int   `json:"total_assignments"`
	ScoredAssignments int   `json:"scored_assignments"`
	TotalReward       int64 `json:"total_reward"`
}

func (s *Store) GetWorkerAggregateStats(ctx context.Context, address string) (*WorkerAggregateStats, error) {
	var st WorkerAggregateStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE((SELECT count(*) FROM questions WHERE questioner = $1), 0),
			COALESCE((SELECT count(*) FROM questions WHERE questioner = $1 AND status = 'scored'), 0),
			COALESCE((SELECT count(*) FROM assignments WHERE worker = $1), 0),
			COALESCE((SELECT count(*) FROM assignments WHERE worker = $1 AND status = 'scored'), 0),
			COALESCE((SELECT sum(final_reward) FROM worker_epoch_rewards WHERE worker_address = $1), 0)
	`, address).Scan(&st.TotalQuestions, &st.ScoredQuestions, &st.TotalAssignments, &st.ScoredAssignments, &st.TotalReward)
	if err != nil {
		return nil, fmt.Errorf("get miner aggregate stats: %w", err)
	}
	return &st, nil
}

type PublicQuestion struct {
	QuestionID int64   `json:"question_id"`
	BSID       string  `json:"bs_id"`
	Questioner string  `json:"questioner"`
	Question   string  `json:"question"`
	Answer     *string `json:"answer,omitempty"`
	Status     string  `json:"status"`
	Score      int     `json:"score"`
	PassRate   float64 `json:"pass_rate"`
	Benchmark  bool    `json:"benchmark"`
	CreatedAt  string  `json:"created_at"`
	ScoredAt   *string `json:"scored_at,omitempty"`
}

type PublicAssignment struct {
	AssignmentID int64   `json:"assignment_id"`
	QuestionID   int64   `json:"question_id"`
	Worker       string  `json:"worker"`
	ReplyValid   *bool   `json:"reply_valid,omitempty"`
	ReplyAnswer  *string `json:"reply_answer,omitempty"`
	Score        int     `json:"score"`
	Share        float64 `json:"share"`
}

func (s *Store) GetRecentQuestions(ctx context.Context, questioner string, limit int) ([]PublicQuestion, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := `
		SELECT question_id, bs_id, questioner, question, answer, status,
			COALESCE(score, 0), COALESCE(pass_rate, 0), benchmark, created_at, scored_at
		FROM questions WHERE status = 'scored'`
	var args []any
	argIdx := 1
	if questioner != "" {
		query += fmt.Sprintf(" AND questioner = $%d", argIdx)
		args = append(args, questioner)
		argIdx++
	}
	query += " ORDER BY scored_at DESC"
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get recent questions: %w", err)
	}
	defer rows.Close()

	var result []PublicQuestion
	for rows.Next() {
		var q PublicQuestion
		var answer string
		var scoredAt *string
		if err := rows.Scan(
			&q.QuestionID, &q.BSID, &q.Questioner, &q.Question, &answer,
			&q.Status, &q.Score, &q.PassRate, &q.Benchmark, &q.CreatedAt, &scoredAt,
		); err != nil {
			return nil, fmt.Errorf("scan public question: %w", err)
		}
		q.Answer = &answer
		q.ScoredAt = scoredAt
		result = append(result, q)
	}
	return result, rows.Err()
}

type PublicAssignmentFilter struct {
	QuestionID int64
	Worker     string
	Limit      int
}

func (s *Store) ListPublicAssignments(ctx context.Context, f PublicAssignmentFilter) ([]PublicAssignment, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	query := `
		SELECT a.assignment_id, a.question_id, a.worker, a.reply_valid, a.reply_answer, a.score, a.share
		FROM assignments a
		JOIN questions q ON q.question_id = a.question_id
		WHERE q.status = 'scored' AND a.status = 'scored'`
	var args []any
	argIdx := 1
	if f.QuestionID > 0 {
		query += fmt.Sprintf(" AND a.question_id = $%d", argIdx)
		args = append(args, f.QuestionID)
		argIdx++
	}
	if f.Worker != "" {
		query += fmt.Sprintf(" AND a.worker = $%d", argIdx)
		args = append(args, f.Worker)
		argIdx++
	}
	query += " ORDER BY a.created_at DESC"
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, f.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list public assignments: %w", err)
	}
	defer rows.Close()

	var result []PublicAssignment
	for rows.Next() {
		var a PublicAssignment
		if err := rows.Scan(&a.AssignmentID, &a.QuestionID, &a.Worker, &a.ReplyValid, &a.ReplyAnswer, &a.Score, &a.Share); err != nil {
			return nil, fmt.Errorf("scan public assignment: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
