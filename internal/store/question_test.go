package store

import (
	"context"
	"testing"

	"github.com/awp-core/subnet-benchmark/internal/minhash"
	"github.com/awp-core/subnet-benchmark/internal/model"
)

func setupQuestionTest(t *testing.T) (*Store, string, string) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()

	// Create BenchmarkSet
	_, err := s.CreateBenchmarkSet(ctx, model.BenchmarkSet{
		SetID: "bs_q_test", Description: "Test", AnswerCheckMethod: "exact", Status: model.BSStatusActive,
	})
	if err != nil {
		t.Fatalf("setup bs: %v", err)
	}
	// Create Miner
	_, err = s.CreateWorker(ctx, "0xquestioner")
	if err != nil {
		t.Fatalf("setup miner: %v", err)
	}
	return s, "bs_q_test", "0xquestioner"
}

func TestCreateQuestion(t *testing.T) {
	s, bsID, questioner := setupQuestionTest(t)
	ctx := context.Background()

	sig := minhash.Generate("What is 2^10 + 3^7?")
	q, err := s.CreateQuestion(ctx, model.Question{
		BSID:       bsID,
		Questioner: questioner,
		Question:   "What is 2^10 + 3^7?",
		Answer:     "3211",
		MinHash:    sig.ToBytes(),
	})
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	if q.QuestionID == 0 {
		t.Error("QuestionID should not be 0")
	}
	if q.Status != model.QuestionStatusSubmitted {
		t.Errorf("Status = %q, want %q", q.Status, model.QuestionStatusSubmitted)
	}
	if q.BSID != bsID {
		t.Errorf("BSID = %q, want %q", q.BSID, bsID)
	}
}

func TestGetQuestion(t *testing.T) {
	s, bsID, questioner := setupQuestionTest(t)
	ctx := context.Background()

	created, _ := s.CreateQuestion(ctx, model.Question{
		BSID: bsID, Questioner: questioner, Question: "test", Answer: "a",
	})

	tests := []struct {
		name    string
		id      int64
		wantNil bool
	}{
		{name: "existing", id: created.QuestionID, wantNil: false},
		{name: "not found", id: 99999, wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := s.GetQuestion(ctx, tt.id)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && q != nil {
				t.Errorf("expected nil, got %+v", q)
			}
			if !tt.wantNil && q == nil {
				t.Fatal("expected non-nil")
			}
		})
	}
}

func TestListQuestionMinHashes(t *testing.T) {
	s, bsID, questioner := setupQuestionTest(t)
	ctx := context.Background()

	sig1 := minhash.Generate("question one")
	sig2 := minhash.Generate("question two")

	// Create 2 questions, score >= 2
	q1, _ := s.CreateQuestion(ctx, model.Question{
		BSID: bsID, Questioner: questioner, Question: "question one", Answer: "a", MinHash: sig1.ToBytes(),
	})
	q2, _ := s.CreateQuestion(ctx, model.Question{
		BSID: bsID, Questioner: questioner, Question: "question two", Answer: "b", MinHash: sig2.ToBytes(),
	})

	// Manually update scores to >= 2
	s.db.ExecContext(ctx, "UPDATE questions SET score = 3 WHERE question_id = $1", q1.QuestionID)
	s.db.ExecContext(ctx, "UPDATE questions SET score = 1 WHERE question_id = $1", q2.QuestionID)

	hashes, err := s.ListQuestionMinHashes(ctx, bsID, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only q1 has score >= 2
	if len(hashes) != 1 {
		t.Errorf("got %d hashes, want 1", len(hashes))
	}
}
