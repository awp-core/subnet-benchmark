package service

import (
	"context"
	"errors"
	"testing"

	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
	"github.com/awp-core/subnet-benchmark/internal/testutil"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(testutil.NewTestDB(t))
}

func setupQuestionService(t *testing.T) (*QuestionService, *store.Store) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()

	s.CreateBenchmarkSet(ctx, model.BenchmarkSet{
		SetID: "bs_test", Description: "Test", AnswerCheckMethod: "exact",
		Status: model.BSStatusActive, QuestionMaxLen: 1000, AnswerMaxLen: 1000,
	})
	s.CreateWorker(ctx, "0xquestioner")

	svc := &QuestionService{
		Store:  s,
		Config: DefaultQuestionConfig(),
	}
	return svc, s
}

func TestSubmitQuestionSuccess(t *testing.T) {
	svc, s := setupQuestionService(t)
	ctx := context.Background()

	result, err := svc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_test", Question: "What is 2^10 + 3^7?", Answer: "3211", Worker: "0xquestioner",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.QuestionID == 0 {
		t.Error("QuestionID should not be 0")
	}

	q, _ := s.GetQuestion(ctx, result.QuestionID)
	if q.Status != model.QuestionStatusSubmitted {
		t.Errorf("question status = %q, want submitted", q.Status)
	}

	bs, _ := s.GetBenchmarkSet(ctx, "bs_test")
	if bs.TotalQuestions != 1 {
		t.Errorf("total_questions = %d, want 1", bs.TotalQuestions)
	}
}

func TestSubmitQuestionInactiveBenchmarkSet(t *testing.T) {
	svc, s := setupQuestionService(t)
	ctx := context.Background()

	s.CreateBenchmarkSet(ctx, model.BenchmarkSet{
		SetID: "bs_inactive", Description: "Inactive", AnswerCheckMethod: "exact", Status: model.BSStatusInactive,
	})

	_, err := svc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_inactive", Question: "test", Answer: "a", Worker: "0xquestioner",
	})
	var ue *UserError
	if !errors.As(err, &ue) || ue.Code != "invalid_bs" {
		t.Errorf("expected UserError with code invalid_bs, got %v", err)
	}
}

func TestSubmitQuestionFieldTooLong(t *testing.T) {
	svc, _ := setupQuestionService(t)
	ctx := context.Background()

	longQ := make([]byte, 1001)
	for i := range longQ {
		longQ[i] = 'a'
	}

	_, err := svc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_test", Question: string(longQ), Answer: "a", Worker: "0xquestioner",
	})
	var ue *UserError
	if !errors.As(err, &ue) || ue.Code != "field_too_long" {
		t.Errorf("expected UserError with code field_too_long, got %v", err)
	}
}

func TestSubmitQuestionRateLimited(t *testing.T) {
	svc, _ := setupQuestionService(t)
	ctx := context.Background()

	_, err := svc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_test", Question: "first question", Answer: "a", Worker: "0xquestioner",
	})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}

	_, err = svc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_test", Question: "second question", Answer: "b", Worker: "0xquestioner",
	})
	var ue *UserError
	if !errors.As(err, &ue) || ue.Code != "rate_limited" {
		t.Errorf("expected rate_limited, got %v", err)
	}
}
