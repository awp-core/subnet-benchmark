package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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

// WorkerTodayStats holds a single worker's stats for today's epoch (UTC).
type WorkerTodayStats struct {
	Address           string  `json:"address"`
	QuestionsAsked    int     `json:"questions_asked"`
	AvgAskScore       float64 `json:"avg_ask_score"`
	QuestionsAnswered int     `json:"questions_answered"`
	TimedOut          int     `json:"timed_out"`
	AvgAnswerScore    float64 `json:"avg_answer_score"`
	CompositeScore    float64 `json:"composite_score"`
	RawReward         int64   `json:"raw_reward"`
	EstimatedReward   int64   `json:"estimated_reward"`
}

// GetWorkerTodayStats returns a worker's performance stats for today's UTC epoch.
func (s *Store) GetWorkerTodayStats(ctx context.Context, address string, totalReward int64, minTasks int) (*WorkerTodayStats, error) {
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	todayEnd := todayStart.Add(24 * time.Hour)

	st := &WorkerTodayStats{Address: address}

	// Questions asked (scored today)
	var askShareSum float64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(count(*), 0), COALESCE(avg(score), 0), COALESCE(sum(share), 0)
		FROM questions
		WHERE questioner = $1 AND status = 'scored'
		AND scored_at >= $2 AND scored_at < $3`,
		address, todayStart, todayEnd,
	).Scan(&st.QuestionsAsked, &st.AvgAskScore, &askShareSum)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("query ask stats: %w", err)
	}

	// Total scored questions today (for reward pool calculation)
	var totalQuestionsToday int
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(count(*), 0) FROM questions
		WHERE status = 'scored' AND scored_at >= $1 AND scored_at < $2`,
		todayStart, todayEnd,
	).Scan(&totalQuestionsToday)
	if err != nil {
		return nil, fmt.Errorf("count today questions: %w", err)
	}

	// Answers (scored today via question's scored_at)
	var ansShareSum float64
	err = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(count(*) FILTER (WHERE a.status = 'scored'), 0),
			COALESCE(count(*) FILTER (WHERE a.status = 'timed-out'), 0),
			COALESCE(avg(a.score) FILTER (WHERE a.status IN ('scored', 'timed-out')), 0),
			COALESCE(sum(a.share) FILTER (WHERE a.status = 'scored'), 0)
		FROM assignments a
		JOIN questions q ON q.question_id = a.question_id
		WHERE a.worker = $1 AND q.status = 'scored'
		AND q.scored_at >= $2 AND q.scored_at < $3`,
		address, todayStart, todayEnd,
	).Scan(&st.QuestionsAnswered, &st.TimedOut, &st.AvgAnswerScore, &ansShareSum)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("query answer stats: %w", err)
	}

	// Compute composite score and estimated reward (mirrors settlement logic)
	hasAsks := st.QuestionsAsked > 0
	hasAnswers := (st.QuestionsAnswered + st.TimedOut) > 0

	if hasAsks && hasAnswers {
		st.CompositeScore = (st.AvgAskScore + st.AvgAnswerScore) / 10.0
	} else if hasAsks {
		st.CompositeScore = st.AvgAskScore / 10.0
	} else if hasAnswers {
		st.CompositeScore = st.AvgAnswerScore / 10.0
	}

	if totalQuestionsToday > 0 {
		// Extrapolate total questions for the full day based on elapsed time.
		elapsed := now.Sub(todayStart).Seconds()
		if elapsed < 60 {
			elapsed = 60 // avoid extreme extrapolation in the first minute
		}
		dayFraction := elapsed / 86400.0
		estimatedTotal := int64(float64(totalQuestionsToday) / dayFraction)
		if estimatedTotal < int64(totalQuestionsToday) {
			estimatedTotal = int64(totalQuestionsToday)
		}

		baseReward := totalReward / estimatedTotal
		askPool := baseReward / 3
		ansPool := baseReward - askPool
		st.RawReward = int64(float64(askPool)*askShareSum) + int64(float64(ansPool)*ansShareSum)
	}

	scoredTasks := st.QuestionsAsked + st.QuestionsAnswered
	if scoredTasks >= minTasks {
		st.EstimatedReward = int64(float64(st.RawReward) * st.CompositeScore)
	}

	return st, nil
}
