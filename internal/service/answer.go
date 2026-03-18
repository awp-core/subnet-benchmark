package service

import (
	"context"
	"fmt"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

type AnswerService struct {
	Store             *store.Store
	OnAnswerSubmitted func(a *model.Assignment) // Callback: cancel timer + trigger scoring check
}

type SubmitAnswerRequest struct {
	QuestionID int64
	Valid      bool
	Answer     string
	Worker     string
}

func (svc *AnswerService) SubmitAnswer(ctx context.Context, req SubmitAnswerRequest) error {
	// 1. Find the miner's assignment for this question
	a, err := svc.Store.GetAssignmentByWorker(ctx, req.QuestionID, req.Worker)
	if err != nil {
		return fmt.Errorf("get assignment: %w", err)
	}
	if a == nil {
		return &UserError{Code: "no_assignment", Message: "no assignment found"}
	}
	if a.Status != model.AssignmentStatusClaimed {
		return &UserError{Code: "no_assignment", Message: "assignment not in claimed state"}
	}
	if time.Now().After(a.ReplyDDL) {
		// Clean up: mark as timed-out so it doesn't block the question
		svc.Store.TimeoutAssignment(ctx, a.AssignmentID)
		return &UserError{Code: "deadline_passed", Message: "reply deadline has passed"}
	}

	// 2. Check answer length
	q, err := svc.Store.GetQuestion(ctx, req.QuestionID)
	if err != nil {
		return fmt.Errorf("get question: %w", err)
	}
	if q == nil {
		return &UserError{Code: "not_found", Message: "question not found"}
	}
	bs, err := svc.Store.GetBenchmarkSet(ctx, q.BSID)
	if err != nil {
		return fmt.Errorf("get benchmark set: %w", err)
	}
	if bs == nil {
		return fmt.Errorf("benchmark set %s not found", q.BSID)
	}
	if len(req.Answer) > bs.AnswerMaxLen {
		return &UserError{Code: "field_too_long", Message: "answer exceeds max length"}
	}

	// 3. Update assignment record
	if err := svc.Store.SetAssignmentReply(ctx, a.AssignmentID, req.Valid, req.Answer); err != nil {
		return fmt.Errorf("set reply: %w", err)
	}

	// 4. Callback: cancel timer, check if all answers collected
	if svc.OnAnswerSubmitted != nil {
		svc.OnAnswerSubmitted(a)
	}

	return nil
}
