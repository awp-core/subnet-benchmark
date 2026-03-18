package service

import (
	"context"
	"fmt"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

type ScoringService struct {
	Store                 *store.Store
	BaseSuspensionMinutes int
	RtConfig              *RuntimeConfig
}

func NewScoringService(s *store.Store) *ScoringService {
	return &ScoringService{Store: s, BaseSuspensionMinutes: 10}
}

func (svc *ScoringService) suspensionThreshold() int {
	if svc.RtConfig != nil {
		return svc.RtConfig.GetSuspensionThreshold()
	}
	return 0
}

func (svc *ScoringService) baseSuspMinutes() int {
	if svc.RtConfig != nil {
		return svc.RtConfig.GetBaseSuspensionMinutes()
	}
	return svc.BaseSuspensionMinutes
}

// ScoreQuestion scores a question and its assignments after all required replies are collected.
func (svc *ScoringService) ScoreQuestion(ctx context.Context, questionID int64) error {
	q, err := svc.Store.GetQuestion(ctx, questionID)
	if err != nil {
		return fmt.Errorf("get question: %w", err)
	}
	if q == nil || q.Status != model.QuestionStatusSubmitted {
		return nil // Already scored or missing
	}

	assignments, err := svc.Store.ListAssignmentsByQuestion(ctx, questionID)
	if err != nil {
		return fmt.Errorf("list assignments: %w", err)
	}

	// Collect replied assignments
	var replied []model.Assignment
	for _, a := range assignments {
		if a.Status == model.AssignmentStatusReplied {
			replied = append(replied, a)
		}
	}
	if len(replied) == 0 {
		return fmt.Errorf("no replied assignments for question %d", questionID)
	}

	// Get BenchmarkSet
	bs, err := svc.Store.GetBenchmarkSet(ctx, q.BSID)
	if err != nil {
		return fmt.Errorf("get benchmark set: %w", err)
	}
	if bs == nil {
		return fmt.Errorf("benchmark set %s not found", q.BSID)
	}

	// Classify answers
	var correctAssigns []model.Assignment
	var wrongAssigns []model.Assignment
	var invalidAssigns []model.Assignment

	for _, a := range replied {
		if a.ReplyValid == nil {
			continue
		}
		if !*a.ReplyValid {
			invalidAssigns = append(invalidAssigns, a)
		} else if checkAnswer(bs.AnswerCheckMethod, q.Answer, deref(a.ReplyAnswer)) {
			correctAssigns = append(correctAssigns, a)
		} else {
			wrongAssigns = append(wrongAssigns, a)
		}
	}

	nCorrect := len(correctAssigns)
	nTotal := len(replied)

	var qScore int
	var qShare float64
	type aScore struct {
		id    int64
		score int
		share float64
	}
	var aScores []aScore

	if nCorrect > 0 {
		// Case 3: some correct
		qScore, qShare = questionerCase3Score(nCorrect)
		correctShare := 1.0 / float64(nCorrect)
		for _, a := range correctAssigns {
			aScores = append(aScores, aScore{a.AssignmentID, 5, correctShare})
		}
		for _, a := range wrongAssigns {
			aScores = append(aScores, aScore{a.AssignmentID, 3, 0})
		}
		for _, a := range invalidAssigns {
			aScores = append(aScores, aScore{a.AssignmentID, 2, 0})
		}
	} else if len(invalidAssigns) == nTotal {
		// Case 1: all judged invalid
		qScore = 0
		qShare = 0
		equalShare := 1.0 / float64(nTotal)
		for _, a := range invalidAssigns {
			aScores = append(aScores, aScore{a.AssignmentID, 5, equalShare})
		}
	} else {
		// Case 2: answers given but none correct
		qScore = 1
		qShare = 0.1

		groups := make(map[string][]model.Assignment)
		var invalidGroup []model.Assignment
		for _, a := range replied {
			if a.ReplyValid == nil {
				continue
			}
			if !*a.ReplyValid {
				invalidGroup = append(invalidGroup, a)
			} else {
				key := "answer:" + deref(a.ReplyAnswer)
				groups[key] = append(groups[key], a)
			}
		}
		if len(invalidGroup) > 0 {
			groups["invalid"] = invalidGroup
		}

		maxSize := 0
		for _, g := range groups {
			if len(g) > maxSize {
				maxSize = len(g)
			}
		}

		var winners []model.Assignment
		losers := make(map[int64]bool)
		for _, g := range groups {
			if len(g) == maxSize {
				winners = append(winners, g...)
			} else {
				for _, a := range g {
					losers[a.AssignmentID] = true
				}
			}
		}

		winnerShare := 1.0 / float64(len(winners))
		for _, a := range winners {
			aScores = append(aScores, aScore{a.AssignmentID, 5, winnerShare})
		}
		for _, a := range replied {
			if losers[a.AssignmentID] {
				aScores = append(aScores, aScore{a.AssignmentID, 2, 0})
			}
		}
	}

	passRate := 0.0
	if nTotal > 0 {
		passRate = float64(nCorrect) / float64(nTotal)
	}

	now := time.Now()
	updated, err := svc.Store.ScoreQuestion(ctx, questionID, qScore, qShare, passRate, now)
	if err != nil {
		return fmt.Errorf("score question: %w", err)
	}
	if !updated {
		return nil
	}

	for _, as := range aScores {
		if err := svc.Store.SetAssignmentScore(ctx, as.id, model.AssignmentStatusScored, as.score, as.share); err != nil {
			return fmt.Errorf("score assignment %d: %w", as.id, err)
		}
	}

	// Suspension (if enabled)
	if qScore < svc.suspensionThreshold() {
		svc.maybeSuspend(ctx, q.Questioner)
	}
	for _, as := range aScores {
		if as.score < svc.suspensionThreshold() {
			a := findAssignByID(assignments, as.id)
			if a != nil {
				svc.maybeSuspend(ctx, a.Worker)
			}
		}
	}

	return nil
}

func (svc *ScoringService) maybeSuspend(ctx context.Context, workerAddr string) {
	if svc.suspensionThreshold() <= 0 {
		return
	}
	wk, err := svc.Store.GetWorker(ctx, workerAddr)
	if err != nil || wk == nil {
		return
	}
	violations := wk.EpochViolations + 1
	minutes := svc.baseSuspMinutes()
	for i := 1; i < violations; i++ {
		minutes *= 2
	}
	until := time.Now().Add(time.Duration(minutes) * time.Minute)
	if wk.IsSuspended() {
		until = wk.SuspendedUntil.Add(time.Duration(minutes) * time.Minute)
	}
	svc.Store.SuspendWorker(ctx, workerAddr, until, violations)
}

func questionerCase3Score(nCorrect int) (score int, share float64) {
	switch nCorrect {
	case 1:
		return 5, 1.0
	case 2:
		return 5, 0.9
	case 3:
		return 4, 0.7
	case 4:
		return 3, 0.5
	default:
		return 2, 0.1
	}
}

func checkAnswer(method, reference, submitted string) bool {
	return reference == submitted
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func findAssignByID(assignments []model.Assignment, id int64) *model.Assignment {
	for i := range assignments {
		if assignments[i].AssignmentID == id {
			return &assignments[i]
		}
	}
	return nil
}
