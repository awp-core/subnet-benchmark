package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

// TimerManager manages per-assignment reply deadline timers.
type TimerManager struct {
	Store           *store.Store
	Scoring         *ScoringService
	RtConfig        *RuntimeConfig
	RequiredReplies int

	timers sync.Map // assignment_id (int64) → *time.Timer
}

func NewTimerManager(s *store.Store, scoring *ScoringService, required int) *TimerManager {
	return &TimerManager{
		Store:           s,
		Scoring:         scoring,
		RequiredReplies: required,
	}
}

// StartReplyTimer starts a reply_ddl timer for a newly created assignment.
func (tm *TimerManager) StartReplyTimer(a *model.Assignment) {
	delay := time.Until(a.ReplyDDL)
	if delay <= 0 {
		go tm.handleTimeout(a.AssignmentID, a.QuestionID, a.Worker)
		return
	}

	timer := time.AfterFunc(delay, func() {
		tm.handleTimeout(a.AssignmentID, a.QuestionID, a.Worker)
	})

	tm.timers.Store(a.AssignmentID, timer)
}

// CancelTimer cancels the timer for the given assignment.
func (tm *TimerManager) CancelTimer(assignmentID int64) {
	if v, ok := tm.timers.LoadAndDelete(assignmentID); ok {
		v.(*time.Timer).Stop()
	}
}

func (tm *TimerManager) handleTimeout(assignmentID, questionID int64, worker string) {
	tm.timers.Delete(assignmentID)

	ctx := context.Background()

	// Check current assignment status
	a, err := tm.Store.GetAssignment(ctx, assignmentID)
	if err != nil {
		log.Printf("timer: get assignment %d: %v", assignmentID, err)
		return
	}
	if a == nil || a.Status != model.AssignmentStatusClaimed {
		return // Already replied or scored
	}

	// Atomically mark as timed-out only if still claimed (prevents race with SubmitAnswer)
	updated, err := tm.Store.TimeoutAssignment(ctx, assignmentID)
	if err != nil {
		log.Printf("timer: timeout assignment %d: %v", assignmentID, err)
		return
	}
	if !updated {
		return // Miner submitted answer just in time
	}

	// Question stays 'submitted' — the freed slot will be picked up by the next poll.
	// Try scoring in case enough replies already exist.
	tm.TryScore(ctx, questionID)
}

// TryScore checks if enough replies are collected to score the question.
func (tm *TimerManager) TryScore(ctx context.Context, questionID int64) {
	assignments, err := tm.Store.ListAssignmentsByQuestion(ctx, questionID)
	if err != nil {
		log.Printf("timer: list assignments for scoring: %v", err)
		return
	}

	repliedCount := 0
	for _, a := range assignments {
		if a.Status == model.AssignmentStatusReplied {
			repliedCount++
		}
	}

	if repliedCount >= tm.RequiredReplies {
		if err := tm.Scoring.ScoreQuestion(ctx, questionID); err != nil {
			log.Printf("timer: score question %d: %v", questionID, err)
		}
	}
}

// RecoverOnStartup rebuilds timers for claimed assignments and handles stale questions.
func (tm *TimerManager) RecoverOnStartup(ctx context.Context) error {
	// Phase 1: Rebuild timers for claimed assignments
	rows, err := tm.Store.DB().QueryContext(ctx, `
		SELECT a.assignment_id, a.question_id, a.worker, a.reply_ddl
		FROM assignments a
		JOIN questions q ON q.question_id = a.question_id
		WHERE q.status = 'submitted'
		AND a.status = 'claimed'`)
	if err != nil {
		return fmt.Errorf("recover timers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var aID, qID int64
		var worker string
		var replyDDL time.Time

		if err := rows.Scan(&aID, &qID, &worker, &replyDDL); err != nil {
			return fmt.Errorf("scan assignment: %w", err)
		}

		a := &model.Assignment{
			AssignmentID: aID,
			QuestionID:   qID,
			Worker:        worker,
			ReplyDDL:     replyDDL,
		}

		if time.Now().After(replyDDL) {
			go tm.handleTimeout(aID, qID, worker)
		} else {
			tm.StartReplyTimer(a)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Phase 2: Try scoring any questions that might have enough replies
	scoreRows, err := tm.Store.DB().QueryContext(ctx, `
		SELECT DISTINCT q.question_id FROM questions q
		WHERE q.status = 'submitted'
		AND (SELECT COUNT(*) FROM assignments a
			WHERE a.question_id = q.question_id AND a.status = 'replied') >= $1`, tm.RequiredReplies)
	if err != nil {
		return fmt.Errorf("find scorable questions: %w", err)
	}
	defer scoreRows.Close()

	for scoreRows.Next() {
		var qID int64
		if err := scoreRows.Scan(&qID); err != nil {
			return fmt.Errorf("scan scorable question: %w", err)
		}
		log.Printf("recover: scoring question %d", qID)
		tm.TryScore(ctx, qID)
	}

	return scoreRows.Err()
}
