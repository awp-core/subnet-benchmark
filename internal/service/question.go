package service

import (
	"context"
	"fmt"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/minhash"
	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

type QuestionConfig struct {
	RequiredAnswers      int
	RateLimit            time.Duration
	ReplyTimeout         time.Duration
	SimilarityMinScore   int
	SimilarityMaxJaccard float64
}

func DefaultQuestionConfig() QuestionConfig {
	return QuestionConfig{
		RequiredAnswers:      5,
		RateLimit:            1 * time.Minute,
		ReplyTimeout:         3 * time.Minute,
		SimilarityMinScore:   2,
		SimilarityMaxJaccard: 0.9,
	}
}

type SubmitQuestionRequest struct {
	BSID     string
	Question string
	Answer   string
	Worker   string
}

type SubmitQuestionResult struct {
	QuestionID int64
}

type QuestionService struct {
	Store    *store.Store
	Config   QuestionConfig
	RtConfig *RuntimeConfig
}

func (svc *QuestionService) cfg() QuestionConfig {
	if svc.RtConfig != nil {
		return svc.RtConfig.QuestionConfig()
	}
	return svc.Config
}

// SubmitQuestion validates and stores a question. No invitations are created —
// questions sit in 'submitted' status until miners pick them up via poll.
func (svc *QuestionService) SubmitQuestion(ctx context.Context, req SubmitQuestionRequest) (*SubmitQuestionResult, error) {
	cfg := svc.cfg()

	// 1. Validate BenchmarkSet
	bs, err := svc.Store.GetBenchmarkSet(ctx, req.BSID)
	if err != nil {
		return nil, fmt.Errorf("get benchmark set: %w", err)
	}
	if bs == nil || bs.Status != model.BSStatusActive {
		return nil, &UserError{Code: "invalid_bs", Message: "benchmark set not found or inactive"}
	}

	// 2. Rate limiting
	if cfg.RateLimit > 0 {
		lastTime, err := svc.Store.GetLastQuestionTime(ctx, req.Worker)
		if err != nil {
			return nil, fmt.Errorf("check rate limit: %w", err)
		}
		if lastTime != nil && time.Since(*lastTime) < cfg.RateLimit {
			return nil, &UserError{Code: "rate_limited", Message: "too many questions, please wait"}
		}
	}

	// 3. Field length check
	if len(req.Question) > bs.QuestionMaxLen {
		return nil, &UserError{Code: "field_too_long", Message: "question exceeds max length"}
	}
	if len(req.Answer) > bs.AnswerMaxLen {
		return nil, &UserError{Code: "field_too_long", Message: "answer exceeds max length"}
	}

	// 4. Similarity check
	newSig := minhash.Generate(req.Question)
	existingHashes, err := svc.Store.ListQuestionMinHashes(ctx, req.BSID, cfg.SimilarityMinScore)
	if err != nil {
		return nil, fmt.Errorf("list minhashes: %w", err)
	}
	for _, h := range existingHashes {
		existingSig := minhash.FromBytes(h)
		sim := minhash.Jaccard(newSig, existingSig)
		if sim >= cfg.SimilarityMaxJaccard {
			return nil, &UserError{Code: "duplicate", Message: "question too similar to an existing one"}
		}
	}

	// 5. Store question + increment counter in transaction
	var result SubmitQuestionResult
	if err := svc.Store.Tx(ctx, func(tx *store.Store) error {
		q, err := tx.CreateQuestion(ctx, model.Question{
			BSID:       req.BSID,
			Questioner: req.Worker,
			Question:   req.Question,
			Answer:     req.Answer,
			MinHash:    newSig.ToBytes(),
		})
		if err != nil {
			return fmt.Errorf("create question: %w", err)
		}
		result.QuestionID = q.QuestionID

		if err := tx.IncrementBSTotalQuestions(ctx, req.BSID); err != nil {
			return fmt.Errorf("increment total: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &result, nil
}
