package service

import (
	"context"
	"fmt"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

type PollConfig struct {
	ReplyTimeout    time.Duration
	RequiredAnswers int
}

func DefaultPollConfig() PollConfig {
	return PollConfig{
		ReplyTimeout:    3 * time.Minute,
		RequiredAnswers: 5,
	}
}

type PollResult struct {
	Assigned *AssignmentView `json:"assigned"`
}

// AssignmentView is the question info sent to a miner when assigned.
type AssignmentView struct {
	AssignmentID         int64  `json:"assignment_id"`
	QuestionID           int64  `json:"question_id"`
	Question             string `json:"question"`
	ReplyDDL             string `json:"reply_ddl"`
	CreatedAt            string `json:"created_at"`
	QuestionRequirements string `json:"question_requirements"`
	AnswerRequirements   string `json:"answer_requirements"`
	AnswerMaxLen         int    `json:"answer_maxlen"`
	Prompt               string `json:"prompt"`
}

const defaultPrompt = "Please judge and answer the question. First judge whether it's valid or not. A valid question should be answerable and meet all the question_requirements that I sent to you. Answer the question with accordance to answer_requirements and answer_maxlen that I sent to you if you think the question is valid."

type PollService struct {
	Store    *store.Store
	Config   PollConfig
	RtConfig *RuntimeConfig
	// OnAssignmentCreated is called after an assignment is created, to start the reply timer.
	OnAssignmentCreated func(a *model.Assignment)
}

func (svc *PollService) replyTimeout() time.Duration {
	if svc.RtConfig != nil {
		return svc.RtConfig.PollConfig().ReplyTimeout
	}
	return svc.Config.ReplyTimeout
}

func (svc *PollService) requiredAnswers() int {
	if svc.RtConfig != nil {
		return svc.RtConfig.QuestionConfig().RequiredAnswers
	}
	return svc.Config.RequiredAnswers
}

func (svc *PollService) answerPrompt() string {
	if svc.RtConfig != nil {
		return svc.RtConfig.GetAnswerPrompt()
	}
	return defaultPrompt
}

// PollOnline handles miner online polling.
// If the miner already has a claimed assignment, return it.
// Otherwise, pick a submitted question and assign it.
func (svc *PollService) PollOnline(ctx context.Context, workerAddr string) (*PollResult, error) {
	// Update last poll time
	if err := svc.Store.UpdateWorkerLastPollAt(ctx, workerAddr, time.Now()); err != nil {
		return nil, fmt.Errorf("update poll time: %w", err)
	}

	// Pick a question and assign it — all within a transaction to prevent races
	required := svc.requiredAnswers()
	replyDDL := time.Now().Add(svc.replyTimeout())

	var assignment *model.Assignment
	if err := svc.Store.Tx(ctx, func(tx *store.Store) error {
		// Check miner doesn't already have a claimed assignment (double-poll guard)
		existing, err := tx.FindClaimedAssignment(ctx, workerAddr)
		if err != nil {
			return fmt.Errorf("check existing: %w", err)
		}
		if existing != nil {
			if time.Now().After(existing.ReplyDDL) {
				// Expired — clean up and pick a new question
				tx.TimeoutAssignment(ctx, existing.AssignmentID)
			} else {
				assignment = existing
				return nil
			}
		}

		// Pick a random submitted question with available slots (FOR UPDATE SKIP LOCKED)
		q, err := tx.PickQuestionForWorker(ctx, workerAddr, required)
		if err != nil {
			return fmt.Errorf("pick question: %w", err)
		}
		if q == nil {
			return nil // No questions available
		}

		// Re-verify count after acquiring the row lock — the CTE's count
		// was a snapshot that may be stale if another transaction committed
		// between CTE evaluation and FOR UPDATE lock acquisition.
		count, err := tx.CountActiveAssignments(ctx, q.QuestionID)
		if err != nil {
			return fmt.Errorf("count active: %w", err)
		}
		if count >= required {
			return nil // Slot was taken by another transaction
		}

		// Create assignment
		a, err := tx.CreateAssignment(ctx, model.Assignment{
			QuestionID: q.QuestionID,
			Worker:     workerAddr,
			ReplyDDL:   replyDDL,
		})
		if err != nil {
			return fmt.Errorf("create assignment: %w", err)
		}
		assignment = a
		return nil
	}); err != nil {
		return nil, err
	}

	if assignment == nil {
		return &PollResult{}, nil
	}

	// Start reply timer after transaction commits
	if svc.OnAssignmentCreated != nil {
		svc.OnAssignmentCreated(assignment)
	}

	return svc.buildAssignmentResult(ctx, assignment)
}

func (svc *PollService) buildAssignmentResult(ctx context.Context, a *model.Assignment) (*PollResult, error) {
	q, err := svc.Store.GetQuestion(ctx, a.QuestionID)
	if err != nil || q == nil {
		return nil, fmt.Errorf("get question %d: %w", a.QuestionID, err)
	}
	bs, err := svc.Store.GetBenchmarkSet(ctx, q.BSID)
	if err != nil || bs == nil {
		return nil, fmt.Errorf("get benchmark set %s: %w", q.BSID, err)
	}

	return &PollResult{
		Assigned: &AssignmentView{
			AssignmentID:         a.AssignmentID,
			QuestionID:           q.QuestionID,
			Question:             q.Question,
			ReplyDDL:             a.ReplyDDL.Format(time.RFC3339),
			CreatedAt:            a.CreatedAt.Format(time.RFC3339),
			QuestionRequirements: bs.QuestionRequirements,
			AnswerRequirements:   bs.AnswerRequirements,
			AnswerMaxLen:         bs.AnswerMaxLen,
			Prompt:               svc.answerPrompt(),
		},
	}, nil
}

