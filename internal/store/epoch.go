package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/awp-core/subnet-benchmark/internal/model"
)

// CreateOrResetEpoch creates an epoch record, or resets it for re-settlement.
func (s *Store) CreateOrResetEpoch(ctx context.Context, epochDate time.Time, totalReward int64) (*model.Epoch, error) {
	var e model.Epoch
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO epochs (epoch_date, total_reward)
		VALUES ($1, $2)
		ON CONFLICT (epoch_date) DO UPDATE SET
			total_reward = EXCLUDED.total_reward,
			total_scored = 0,
			settled_at = NULL,
			merkle_root = NULL,
			published_at = NULL
		RETURNING epoch_date, total_reward, total_scored, settled_at, merkle_root, published_at`,
		epochDate, totalReward,
	).Scan(&e.EpochDate, &e.TotalReward, &e.TotalScored, &e.SettledAt, &e.MerkleRoot, &e.PublishedAt)
	if err != nil {
		return nil, fmt.Errorf("create or reset epoch: %w", err)
	}
	return &e, nil
}

// DeleteEpochRewards removes all worker_epoch_rewards for an epoch (for re-settlement).
func (s *Store) DeleteEpochRewards(ctx context.Context, epochDate time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM worker_epoch_rewards WHERE epoch_date = $1`, epochDate)
	if err != nil {
		return fmt.Errorf("delete epoch rewards: %w", err)
	}
	return nil
}

// DeleteEpochMerkleProofs removes all merkle_proofs for an epoch (for re-settlement).
func (s *Store) DeleteEpochMerkleProofs(ctx context.Context, epochDate time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM merkle_proofs WHERE epoch_date = $1`, epochDate)
	if err != nil {
		return fmt.Errorf("delete epoch merkle proofs: %w", err)
	}
	return nil
}

// ResetBenchmarkFlags unmarks benchmark questions scored in the given epoch window (for re-settlement).
func (s *Store) ResetBenchmarkFlags(ctx context.Context, start, end time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE questions SET benchmark = false
		WHERE status = 'scored' AND scored_at >= $1 AND scored_at < $2 AND benchmark = true`, start, end)
	if err != nil {
		return fmt.Errorf("reset benchmark flags: %w", err)
	}
	return nil
}

// FinishEpoch sets the total_scored and settled_at for an epoch.
func (s *Store) FinishEpoch(ctx context.Context, epochDate time.Time, totalScored int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE epochs SET total_scored = $1, settled_at = now()
		WHERE epoch_date = $2`, totalScored, epochDate)
	if err != nil {
		return fmt.Errorf("finish epoch: %w", err)
	}
	return nil
}

// ListScoredQuestionsInRange returns scored questions with scored_at in [start, end).
func (s *Store) ListScoredQuestionsInRange(ctx context.Context, start, end time.Time) ([]model.Question, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT question_id, bs_id, questioner, question, answer, status,
			share, score, pass_rate, benchmark, minhash, created_at, scored_at
		FROM questions
		WHERE status = 'scored' AND scored_at >= $1 AND scored_at < $2
		ORDER BY scored_at`, start, end)
	if err != nil {
		return nil, fmt.Errorf("list scored questions: %w", err)
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

// ListScoredAssignmentsForQuestions returns all scored or timed-out assignments for the given question IDs.
func (s *Store) ListScoredAssignmentsForQuestions(ctx context.Context, questionIDs []int64) ([]model.Assignment, error) {
	if len(questionIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT assignment_id, question_id, worker, status, reply_ddl,
			reply_valid, reply_answer, replied_at, share, score, created_at
		FROM assignments
		WHERE question_id = ANY($1) AND status IN ('scored', 'timed-out')
		ORDER BY question_id, created_at`, pq.Array(questionIDs))
	if err != nil {
		return nil, fmt.Errorf("list scored assignments: %w", err)
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

// CountInvalidReports returns the count of scored assignments that judged the question as invalid.
func (s *Store) CountInvalidReports(ctx context.Context, questionID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM assignments
		WHERE question_id = $1 AND status = 'scored' AND reply_valid = false`, questionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count invalid reports: %w", err)
	}
	return count, nil
}

// MarkBenchmark marks a question as benchmark and increments the qualified_questions counter.
func (s *Store) MarkBenchmark(ctx context.Context, questionID int64, bsID string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE questions SET benchmark = true WHERE question_id = $1`, questionID); err != nil {
		return fmt.Errorf("mark benchmark: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE benchmark_sets SET qualified_questions = qualified_questions + 1 WHERE set_id = $1`, bsID); err != nil {
		return fmt.Errorf("increment qualified: %w", err)
	}
	return nil
}

// SaveWorkerEpochReward saves a miner's epoch reward record.
func (s *Store) SaveWorkerEpochReward(ctx context.Context, r model.WorkerEpochReward) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO worker_epoch_rewards (
			epoch_date, worker_address, recipient, scored_asks, scored_answers, timedout_answers,
			ask_score_sum, answer_score_sum, raw_reward, composite_score, final_reward
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		r.EpochDate, r.WorkerAddress, r.Recipient, r.ScoredAsks, r.ScoredAnswers, r.TimedOutAnswers,
		r.AskScoreSum, r.AnswerScoreSum, r.RawReward, r.CompositeScore, r.FinalReward)
	if err != nil {
		return fmt.Errorf("save miner epoch reward: %w", err)
	}
	return nil
}

// BatchSaveWorkerEpochRewards inserts multiple epoch rewards in a single multi-row INSERT.
func (s *Store) BatchSaveWorkerEpochRewards(ctx context.Context, rewards []model.WorkerEpochReward) error {
	if len(rewards) == 0 {
		return nil
	}

	const batchSize = 100
	for i := 0; i < len(rewards); i += batchSize {
		end := i + batchSize
		if end > len(rewards) {
			end = len(rewards)
		}
		batch := rewards[i:end]

		query := `INSERT INTO worker_epoch_rewards (
			epoch_date, worker_address, recipient, scored_asks, scored_answers, timedout_answers,
			ask_score_sum, answer_score_sum, raw_reward, composite_score, final_reward
		) VALUES `
		args := make([]any, 0, len(batch)*11)
		for j, r := range batch {
			if j > 0 {
				query += ", "
			}
			base := j * 11
			query += fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
				base+1, base+2, base+3, base+4, base+5, base+6,
				base+7, base+8, base+9, base+10, base+11)
			args = append(args, r.EpochDate, r.WorkerAddress, r.Recipient,
				r.ScoredAsks, r.ScoredAnswers, r.TimedOutAnswers,
				r.AskScoreSum, r.AnswerScoreSum, r.RawReward,
				r.CompositeScore, r.FinalReward)
		}

		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("batch save worker epoch rewards: %w", err)
		}
	}
	return nil
}

// ListWorkerEpochRewards returns all epoch reward records for the given miner, newest first.
func (s *Store) ListWorkerEpochRewards(ctx context.Context, workerAddress string) ([]model.WorkerEpochReward, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT epoch_date, worker_address, recipient, scored_asks, scored_answers, timedout_answers,
			ask_score_sum, answer_score_sum, raw_reward, composite_score, final_reward
		FROM worker_epoch_rewards WHERE worker_address = $1
		ORDER BY epoch_date DESC`, workerAddress)
	if err != nil {
		return nil, fmt.Errorf("list miner epoch rewards: %w", err)
	}
	defer rows.Close()

	var result []model.WorkerEpochReward
	for rows.Next() {
		var r model.WorkerEpochReward
		if err := rows.Scan(
			&r.EpochDate, &r.WorkerAddress, &r.Recipient, &r.ScoredAsks, &r.ScoredAnswers, &r.TimedOutAnswers,
			&r.AskScoreSum, &r.AnswerScoreSum, &r.RawReward, &r.CompositeScore, &r.FinalReward,
		); err != nil {
			return nil, fmt.Errorf("scan miner epoch reward: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ListRecipientEpochRewards returns all epoch reward records for a given recipient, newest first.
func (s *Store) ListRecipientEpochRewards(ctx context.Context, recipient string) ([]model.WorkerEpochReward, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT epoch_date, worker_address, recipient, scored_asks, scored_answers, timedout_answers,
			ask_score_sum, answer_score_sum, raw_reward, composite_score, final_reward
		FROM worker_epoch_rewards WHERE recipient = $1
		ORDER BY epoch_date DESC, worker_address`, recipient)
	if err != nil {
		return nil, fmt.Errorf("list recipient epoch rewards: %w", err)
	}
	defer rows.Close()

	var result []model.WorkerEpochReward
	for rows.Next() {
		var r model.WorkerEpochReward
		if err := rows.Scan(
			&r.EpochDate, &r.WorkerAddress, &r.Recipient, &r.ScoredAsks, &r.ScoredAnswers, &r.TimedOutAnswers,
			&r.AskScoreSum, &r.AnswerScoreSum, &r.RawReward, &r.CompositeScore, &r.FinalReward,
		); err != nil {
			return nil, fmt.Errorf("scan recipient epoch reward: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetWorkerEpochReward returns a single epoch reward record for the given miner and date.
func (s *Store) GetWorkerEpochReward(ctx context.Context, workerAddress string, epochDate time.Time) (*model.WorkerEpochReward, error) {
	var r model.WorkerEpochReward
	err := s.db.QueryRowContext(ctx, `
		SELECT epoch_date, worker_address, recipient, scored_asks, scored_answers, timedout_answers,
			ask_score_sum, answer_score_sum, raw_reward, composite_score, final_reward
		FROM worker_epoch_rewards WHERE worker_address = $1 AND epoch_date = $2`,
		workerAddress, epochDate,
	).Scan(
		&r.EpochDate, &r.WorkerAddress, &r.Recipient, &r.ScoredAsks, &r.ScoredAnswers, &r.TimedOutAnswers,
		&r.AskScoreSum, &r.AnswerScoreSum, &r.RawReward, &r.CompositeScore, &r.FinalReward,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get miner epoch reward: %w", err)
	}
	return &r, nil
}

// SetWorkerEpochRecipient updates the recipient for a miner's epoch reward.
func (s *Store) SetWorkerEpochRecipient(ctx context.Context, epochDate time.Time, workerAddress, recipient string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE worker_epoch_rewards SET recipient = $1
		WHERE epoch_date = $2 AND worker_address = $3`,
		recipient, epochDate, workerAddress)
	if err != nil {
		return fmt.Errorf("set miner epoch recipient: %w", err)
	}
	return nil
}

// ResetAllEpochViolations resets epoch_violations to 0 for all miners.
func (s *Store) ResetAllEpochViolations(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workers SET epoch_violations = 0`)
	if err != nil {
		return fmt.Errorf("reset epoch violations: %w", err)
	}
	return nil
}
