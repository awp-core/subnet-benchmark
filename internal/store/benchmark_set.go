package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/awp-core/subnet-benchmark/internal/model"
)

func (s *Store) CreateBenchmarkSet(ctx context.Context, bs model.BenchmarkSet) (*model.BenchmarkSet, error) {
	var out model.BenchmarkSet
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO benchmark_sets (set_id, description, question_requirements, answer_requirements,
			question_maxlen, answer_maxlen, answer_check_method, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING set_id, description, question_requirements, answer_requirements,
			question_maxlen, answer_maxlen, answer_check_method, status,
			total_questions, qualified_questions, created_at`,
		bs.SetID, bs.Description, bs.QuestionRequirements, bs.AnswerRequirements,
		bs.QuestionMaxLen, bs.AnswerMaxLen, bs.AnswerCheckMethod, bs.Status,
	).Scan(
		&out.SetID, &out.Description, &out.QuestionRequirements, &out.AnswerRequirements,
		&out.QuestionMaxLen, &out.AnswerMaxLen, &out.AnswerCheckMethod, &out.Status,
		&out.TotalQuestions, &out.QualifiedQuestions, &out.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create benchmark set: %w", err)
	}
	return &out, nil
}

func (s *Store) GetBenchmarkSet(ctx context.Context, setID string) (*model.BenchmarkSet, error) {
	var out model.BenchmarkSet
	err := s.db.QueryRowContext(ctx, `
		SELECT set_id, description, question_requirements, answer_requirements,
			question_maxlen, answer_maxlen, answer_check_method, status,
			total_questions, qualified_questions, created_at
		FROM benchmark_sets WHERE set_id = $1`, setID,
	).Scan(
		&out.SetID, &out.Description, &out.QuestionRequirements, &out.AnswerRequirements,
		&out.QuestionMaxLen, &out.AnswerMaxLen, &out.AnswerCheckMethod, &out.Status,
		&out.TotalQuestions, &out.QualifiedQuestions, &out.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get benchmark set: %w", err)
	}
	return &out, nil
}

func (s *Store) ListBenchmarkSets(ctx context.Context, status string) ([]model.BenchmarkSet, error) {
	query := `
		SELECT set_id, description, question_requirements, answer_requirements,
			question_maxlen, answer_maxlen, answer_check_method, status,
			total_questions, qualified_questions, created_at
		FROM benchmark_sets`
	var args []any
	if status != "" {
		query += " WHERE status = $1"
		args = append(args, status)
	}
	query += " ORDER BY created_at"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list benchmark sets: %w", err)
	}
	defer rows.Close()

	var result []model.BenchmarkSet
	for rows.Next() {
		var bs model.BenchmarkSet
		if err := rows.Scan(
			&bs.SetID, &bs.Description, &bs.QuestionRequirements, &bs.AnswerRequirements,
			&bs.QuestionMaxLen, &bs.AnswerMaxLen, &bs.AnswerCheckMethod, &bs.Status,
			&bs.TotalQuestions, &bs.QualifiedQuestions, &bs.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan benchmark set: %w", err)
		}
		result = append(result, bs)
	}
	return result, rows.Err()
}

// allowedBSFields maps allowed update field names to their database column names.
var allowedBSFields = map[string]string{
	"description":           "description",
	"question_requirements": "question_requirements",
	"answer_requirements":   "answer_requirements",
	"question_maxlen":       "question_maxlen",
	"answer_maxlen":         "answer_maxlen",
	"answer_check_method":   "answer_check_method",
	"status":                "status",
}

func (s *Store) UpdateBenchmarkSet(ctx context.Context, setID string, fields map[string]any) (*model.BenchmarkSet, error) {
	if len(fields) == 0 {
		return s.GetBenchmarkSet(ctx, setID)
	}

	var setClauses []string
	var args []any
	argIdx := 1
	for key, val := range fields {
		col, ok := allowedBSFields[key]
		if !ok {
			return nil, fmt.Errorf("update benchmark set: unknown field %q", key)
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}
	args = append(args, setID)

	query := fmt.Sprintf(`
		UPDATE benchmark_sets SET %s
		WHERE set_id = $%d
		RETURNING set_id, description, question_requirements, answer_requirements,
			question_maxlen, answer_maxlen, answer_check_method, status,
			total_questions, qualified_questions, created_at`,
		strings.Join(setClauses, ", "), argIdx)

	var out model.BenchmarkSet
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&out.SetID, &out.Description, &out.QuestionRequirements, &out.AnswerRequirements,
		&out.QuestionMaxLen, &out.AnswerMaxLen, &out.AnswerCheckMethod, &out.Status,
		&out.TotalQuestions, &out.QualifiedQuestions, &out.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("update benchmark set: not found")
	}
	if err != nil {
		return nil, fmt.Errorf("update benchmark set: %w", err)
	}
	return &out, nil
}
