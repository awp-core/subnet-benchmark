package service

import (
	"context"
	"testing"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

func setupFlowTest(t *testing.T) (*PollService, *QuestionService, *AnswerService, *ScoringService, *TimerManager, *store.Store) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()

	s.CreateBenchmarkSet(ctx, model.BenchmarkSet{
		SetID: "bs_flow", Description: "Flow Test", AnswerCheckMethod: "exact",
		Status: model.BSStatusActive, QuestionMaxLen: 1000, AnswerMaxLen: 1000,
	})

	scoringSvc := NewScoringService(s)
	timerMgr := NewTimerManager(s, scoringSvc, 5)

	questionSvc := &QuestionService{Store: s, Config: DefaultQuestionConfig()}

	pollSvc := &PollService{
		Store:               s,
		Config:              PollConfig{ReplyTimeout: 3 * time.Minute, RequiredAnswers: 5},
		OnAssignmentCreated: timerMgr.StartReplyTimer,
	}

	answerSvc := &AnswerService{
		Store: s,
		OnAnswerSubmitted: func(a *model.Assignment) {
			timerMgr.CancelTimer(a.AssignmentID)
			timerMgr.TryScore(context.Background(), a.QuestionID)
		},
	}

	return pollSvc, questionSvc, answerSvc, scoringSvc, timerMgr, s
}

func createWorkers(t *testing.T, s *store.Store, n int) []string {
	t.Helper()
	ctx := context.Background()
	addrs := make([]string, n)
	for i := 0; i < n; i++ {
		addr := "0x" + string(rune('a'+i)) + "000000000000000000000000000000000000000"
		s.CreateWorker(ctx, addr)
		addrs[i] = addr
	}
	return addrs
}

func TestFlow_SubmitAndPoll(t *testing.T) {
	pollSvc, qSvc, _, _, _, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 7)

	result, err := qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "What is 1+1?", Answer: "2", Worker: workers[0],
	})
	if err != nil {
		t.Fatalf("submit question: %v", err)
	}

	q, _ := s.GetQuestion(ctx, result.QuestionID)
	if q.Status != model.QuestionStatusSubmitted {
		t.Errorf("question status = %q, want submitted", q.Status)
	}

	r, err := pollSvc.PollOnline(ctx, workers[1])
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if r.Assigned == nil {
		t.Fatal("expected assigned question")
	}
	if r.Assigned.QuestionID != result.QuestionID {
		t.Errorf("question_id = %d, want %d", r.Assigned.QuestionID, result.QuestionID)
	}
}

func TestFlow_QuestionnersOwnQuestionExcluded(t *testing.T) {
	pollSvc, qSvc, _, _, _, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 2)

	qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "My question", Answer: "x", Worker: workers[0],
	})

	r, err := pollSvc.PollOnline(ctx, workers[0])
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if r.Assigned != nil {
		t.Error("questioner should not get their own question")
	}
}

func TestFlow_NoDoubleAssignment(t *testing.T) {
	pollSvc, qSvc, _, _, _, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 3)

	qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "Test?", Answer: "yes", Worker: workers[0],
	})

	r1, _ := pollSvc.PollOnline(ctx, workers[1])
	r2, _ := pollSvc.PollOnline(ctx, workers[1])

	if r1.Assigned == nil || r2.Assigned == nil {
		t.Fatal("expected assignments")
	}
	if r1.Assigned.AssignmentID != r2.Assigned.AssignmentID {
		t.Errorf("double poll returned different assignments: %d vs %d",
			r1.Assigned.AssignmentID, r2.Assigned.AssignmentID)
	}
}

func TestFlow_ExactlyFiveAssignments(t *testing.T) {
	pollSvc, qSvc, _, _, _, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 8)

	qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "Count test", Answer: "5", Worker: workers[0],
	})

	assigned := 0
	for i := 1; i <= 7; i++ {
		r, err := pollSvc.PollOnline(ctx, workers[i])
		if err != nil {
			t.Fatalf("miner %d poll: %v", i, err)
		}
		if r.Assigned != nil {
			assigned++
		}
	}
	if assigned != 5 {
		t.Errorf("assigned = %d, want 5", assigned)
	}
}

func TestFlow_FullCycleWithScoring(t *testing.T) {
	pollSvc, qSvc, ansSvc, _, _, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 7)

	result, _ := qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "What is 2+2?", Answer: "4", Worker: workers[0],
	})
	qID := result.QuestionID

	for i := 1; i <= 5; i++ {
		pollSvc.PollOnline(ctx, workers[i])
	}

	for i := 1; i <= 5; i++ {
		answer := "4"
		if i > 3 {
			answer = "5"
		}
		if err := ansSvc.SubmitAnswer(ctx, SubmitAnswerRequest{
			QuestionID: qID, Valid: true, Answer: answer, Worker: workers[i],
		}); err != nil {
			t.Fatalf("miner %d answer: %v", i, err)
		}
	}

	q, _ := s.GetQuestion(ctx, qID)
	if q.Status != model.QuestionStatusScored {
		t.Errorf("question status = %q, want scored", q.Status)
	}
	if q.Score == 0 {
		t.Error("question score should not be 0")
	}

	assignments, _ := s.ListAssignmentsByQuestion(ctx, qID)
	scored := 0
	for _, a := range assignments {
		if a.Status == model.AssignmentStatusScored {
			scored++
		}
	}
	if scored != 5 {
		t.Errorf("scored assignments = %d, want 5", scored)
	}
}

func TestFlow_TimeoutFreesSlot(t *testing.T) {
	pollSvc, qSvc, ansSvc, _, timerMgr, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 8)

	pollSvc.Config.ReplyTimeout = 1 * time.Second

	result, _ := qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "Timeout test", Answer: "x", Worker: workers[0],
	})
	qID := result.QuestionID

	for i := 1; i <= 5; i++ {
		pollSvc.PollOnline(ctx, workers[i])
	}

	// Miner 6 — all slots taken
	r, _ := pollSvc.PollOnline(ctx, workers[6])
	if r.Assigned != nil {
		t.Error("miner 6 should not get assignment when all slots taken")
	}

	// Miners 1-3 answer
	for i := 1; i <= 3; i++ {
		ansSvc.SubmitAnswer(ctx, SubmitAnswerRequest{
			QuestionID: qID, Valid: true, Answer: "x", Worker: workers[i],
		})
	}

	// Wait for timeout
	time.Sleep(2 * time.Second)

	// Force timeout for expired assignments
	assignments, _ := s.ListAssignmentsByQuestion(ctx, qID)
	for _, a := range assignments {
		if a.Status == model.AssignmentStatusClaimed && time.Now().After(a.ReplyDDL) {
			timerMgr.Store.TimeoutAssignment(ctx, a.AssignmentID)
		}
	}

	// Miners 6 and 7 pick up freed slots
	r, _ = pollSvc.PollOnline(ctx, workers[6])
	if r.Assigned == nil {
		t.Error("miner 6 should get assignment after timeout")
	}
	r, _ = pollSvc.PollOnline(ctx, workers[7])
	if r.Assigned == nil {
		t.Error("miner 7 should get assignment after timeout")
	}

	// Submit remaining answers
	ansSvc.SubmitAnswer(ctx, SubmitAnswerRequest{QuestionID: qID, Valid: true, Answer: "x", Worker: workers[6]})
	ansSvc.SubmitAnswer(ctx, SubmitAnswerRequest{QuestionID: qID, Valid: true, Answer: "x", Worker: workers[7]})

	q, _ := s.GetQuestion(ctx, qID)
	if q.Status != model.QuestionStatusScored {
		t.Errorf("question status = %q, want scored", q.Status)
	}
}

func TestFlow_AlreadyAnsweredExcluded(t *testing.T) {
	pollSvc, qSvc, ansSvc, _, _, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 3)

	result, _ := qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "First", Answer: "y", Worker: workers[0],
	})

	pollSvc.PollOnline(ctx, workers[1])
	ansSvc.SubmitAnswer(ctx, SubmitAnswerRequest{
		QuestionID: result.QuestionID, Valid: true, Answer: "y", Worker: workers[1],
	})

	// Backdate first question to bypass rate limit
	s.DB().ExecContext(ctx, "UPDATE questions SET created_at = now() - interval '2 minutes' WHERE question_id = $1", result.QuestionID)

	result2, err := qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "Second", Answer: "z", Worker: workers[0],
	})
	if err != nil {
		t.Fatalf("submit second question: %v", err)
	}

	r, _ := pollSvc.PollOnline(ctx, workers[1])
	if r.Assigned == nil {
		t.Fatal("expected assignment")
	}
	if r.Assigned.QuestionID == result.QuestionID {
		t.Error("should not be re-assigned to already answered question")
	}
	if r.Assigned.QuestionID != result2.QuestionID {
		t.Errorf("expected question %d, got %d", result2.QuestionID, r.Assigned.QuestionID)
	}
}

func TestFlow_IdleWhenNoQuestions(t *testing.T) {
	pollSvc, _, _, _, _, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 1)

	r, err := pollSvc.PollOnline(ctx, workers[0])
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if r.Assigned != nil {
		t.Error("expected nil assigned when no questions")
	}
}

func TestFlow_ScoringCase1AllInvalid(t *testing.T) {
	pollSvc, qSvc, ansSvc, _, _, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 7)

	result, _ := qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "Bad?", Answer: "whatever", Worker: workers[0],
	})

	for i := 1; i <= 5; i++ {
		pollSvc.PollOnline(ctx, workers[i])
		ansSvc.SubmitAnswer(ctx, SubmitAnswerRequest{
			QuestionID: result.QuestionID, Valid: false, Answer: "", Worker: workers[i],
		})
	}

	q, _ := s.GetQuestion(ctx, result.QuestionID)
	if q.Status != model.QuestionStatusScored {
		t.Fatalf("status = %q, want scored", q.Status)
	}
	if q.Score != 0 {
		t.Errorf("score = %d, want 0", q.Score)
	}
}

func TestFlow_ScoringCase3SomeCorrect(t *testing.T) {
	pollSvc, qSvc, ansSvc, _, _, s := setupFlowTest(t)
	ctx := context.Background()
	workers := createWorkers(t, s, 7)

	result, _ := qSvc.SubmitQuestion(ctx, SubmitQuestionRequest{
		BSID: "bs_flow", Question: "3*3?", Answer: "9", Worker: workers[0],
	})

	for i := 1; i <= 5; i++ {
		pollSvc.PollOnline(ctx, workers[i])
		answer := "9"
		if i > 2 {
			answer = "8"
		}
		ansSvc.SubmitAnswer(ctx, SubmitAnswerRequest{
			QuestionID: result.QuestionID, Valid: true, Answer: answer, Worker: workers[i],
		})
	}

	q, _ := s.GetQuestion(ctx, result.QuestionID)
	if q.Status != model.QuestionStatusScored {
		t.Fatalf("status = %q, want scored", q.Status)
	}
	if q.Score != 5 {
		t.Errorf("score = %d, want 5 (2 correct)", q.Score)
	}

	assignments, _ := s.ListAssignmentsByQuestion(ctx, result.QuestionID)
	for _, a := range assignments {
		if a.Status != model.AssignmentStatusScored {
			continue
		}
		if a.Worker == workers[1] || a.Worker == workers[2] {
			if a.Score != 5 {
				t.Errorf("correct answerer %s score = %d, want 5", a.Worker, a.Score)
			}
		} else {
			if a.Score != 3 {
				t.Errorf("wrong answerer %s score = %d, want 3", a.Worker, a.Score)
			}
		}
	}
}
