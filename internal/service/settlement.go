package service

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/awp-core/subnet-benchmark/internal/merkle"
	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

type SettlementConfig struct {
	TotalReward  int64 // Total reward pool per epoch
	MinTasks     int   // Minimum completed tasks
	BenchmarkMinScore float64 // Min composite score for benchmark questioner
}

func DefaultSettlementConfig() SettlementConfig {
	return SettlementConfig{
		TotalReward:       1000000,
		MinTasks:          10,
		BenchmarkMinScore: 0.6,
	}
}

type SettlementService struct {
	Store    *store.Store
	Config   SettlementConfig
	RtConfig *RuntimeConfig
	RootNet  *RootNetClient  // Required: for querying reward recipient addresses
	Onchain  *OnchainService // Optional: if set, publishes merkle root on-chain after settlement
}

func (svc *SettlementService) cfg() SettlementConfig {
	if svc.RtConfig != nil {
		return svc.RtConfig.SettlementConfig()
	}
	return svc.Config
}

// minerStats holds per-miner statistics for the current epoch.
type minerStats struct {
	scoredAsks     int
	scoredAnswers  int
	timedOutAnswers int
	askScoreSum    int
	answerScoreSum int
	rawReward      int64
}

// Settle performs epoch settlement for the given date (UTC 00:00:00).
func (svc *SettlementService) Settle(ctx context.Context, epochDate time.Time) error {
	cfg := svc.cfg()

	// 1. Create epoch record
	_, err := svc.Store.CreateEpoch(ctx, epochDate, cfg.TotalReward)
	if err != nil {
		return fmt.Errorf("create epoch: %w", err)
	}

	// 2. Query all scored questions for this epoch
	start := epochDate
	end := epochDate.Add(24 * time.Hour)
	questions, err := svc.Store.ListScoredQuestionsInRange(ctx, start, end)
	if err != nil {
		return fmt.Errorf("list questions: %w", err)
	}

	totalQuestions := len(questions)
	if totalQuestions == 0 {
		if err := svc.Store.FinishEpoch(ctx, epochDate, 0); err != nil {
			return fmt.Errorf("finish epoch: %w", err)
		}
		return nil
	}

	// 3. Query all related invitations
	qIDs := make([]int64, len(questions))
	for i, q := range questions {
		qIDs[i] = q.QuestionID
	}
	assignments, err := svc.Store.ListScoredAssignmentsForQuestions(ctx, qIDs)
	if err != nil {
		return fmt.Errorf("list assignments: %w", err)
	}

	// Index assignments by question_id
	aByQ := make(map[int64][]model.Assignment)
	for _, a := range assignments {
		aByQ[a.QuestionID] = append(aByQ[a.QuestionID], a)
	}

	// 4. Compute per-question base_reward
	baseReward := cfg.TotalReward / int64(totalQuestions)
	askPool := baseReward / 3
	ansPool := baseReward - askPool // 2/3, avoids integer division error

	// 5. Aggregate per-miner statistics
	stats := make(map[string]*minerStats)
	ensureStats := func(addr string) *minerStats {
		if s, ok := stats[addr]; ok {
			return s
		}
		s := &minerStats{}
		stats[addr] = s
		return s
	}

	for _, q := range questions {
		ms := ensureStats(q.Questioner)
		ms.scoredAsks++
		ms.askScoreSum += q.Score
		ms.rawReward += int64(float64(askPool) * q.Share)

		for _, a := range aByQ[q.QuestionID] {
			is := ensureStats(a.Worker)
			if a.Status == model.AssignmentStatusScored {
				is.scoredAnswers++
			} else if a.Status == model.AssignmentStatusTimedOut {
				is.timedOutAnswers++
			}
			is.answerScoreSum += a.Score
			is.rawReward += int64(float64(ansPool) * a.Share)
		}
	}

	// 6. Compute composite score and final reward, save to database
	minerComposites := make(map[string]float64) // For benchmark judgment
	minerFinalRewards := make(map[string]int64) // For merkle tree
	var rewards []model.WorkerEpochReward

	for addr, ms := range stats {
		var askAvg, ansAvg float64
		hasAsks := ms.scoredAsks > 0
		hasAnswers := (ms.scoredAnswers + ms.timedOutAnswers) > 0

		if hasAsks {
			askAvg = float64(ms.askScoreSum) / float64(ms.scoredAsks)
		}
		if hasAnswers {
			ansAvg = float64(ms.answerScoreSum) / float64(ms.scoredAnswers+ms.timedOutAnswers)
		}

		var composite float64
		if hasAsks && hasAnswers {
			composite = (askAvg + ansAvg) / 10.0 // Max 1.0, favors dual-role miners
		} else if hasAsks {
			composite = askAvg / 10.0 // Max 0.5
		} else if hasAnswers {
			composite = ansAvg / 10.0 // Max 0.5
		}

		scoredTasks := ms.scoredAsks + ms.scoredAnswers
		var finalReward int64
		if scoredTasks >= cfg.MinTasks {
			finalReward = int64(float64(ms.rawReward) * composite)
		}

		minerComposites[addr] = composite
		minerFinalRewards[addr] = finalReward

		rewards = append(rewards, model.WorkerEpochReward{
			EpochDate:       epochDate,
			WorkerAddress:    addr,
			ScoredAsks:      ms.scoredAsks,
			ScoredAnswers:   ms.scoredAnswers,
			TimedOutAnswers: ms.timedOutAnswers,
			AskScoreSum:     ms.askScoreSum,
			AnswerScoreSum:  ms.answerScoreSum,
			RawReward:       ms.rawReward,
			CompositeScore:  composite,
			FinalReward:     finalReward,
		})
	}

	if err := svc.Store.BatchSaveWorkerEpochRewards(ctx, rewards); err != nil {
		return fmt.Errorf("batch save rewards: %w", err)
	}

	// 7. Benchmark judgment
	for _, q := range questions {
		if q.Score < 4 {
			continue
		}
		invalidCount, err := svc.Store.CountInvalidReports(ctx, q.QuestionID)
		if err != nil {
			return fmt.Errorf("count invalid for q%d: %w", q.QuestionID, err)
		}
		if invalidCount > 1 {
			continue
		}
		if minerComposites[q.Questioner] < cfg.BenchmarkMinScore {
			continue
		}
		if err := svc.Store.MarkBenchmark(ctx, q.QuestionID, q.BSID); err != nil {
			return fmt.Errorf("mark benchmark q%d: %w", q.QuestionID, err)
		}
	}

	// 8. Build Merkle tree and store proofs
	if err := svc.buildMerkleTree(ctx, epochDate, minerFinalRewards); err != nil {
		return fmt.Errorf("build merkle tree: %w", err)
	}

	// 9. Finish epoch (mark settled before on-chain publish, so settlement is complete
	//    even if publishing fails — admin can retry publishing separately)
	if err := svc.Store.FinishEpoch(ctx, epochDate, totalQuestions); err != nil {
		return fmt.Errorf("finish epoch: %w", err)
	}

	// 10. Reset violation counts
	if err := svc.Store.ResetAllEpochViolations(ctx); err != nil {
		return fmt.Errorf("reset violations: %w", err)
	}

	// 11. Publish merkle root on-chain (non-fatal — settlement data is already persisted)
	if svc.Onchain != nil && !svc.isTestnet() {
		if err := svc.Onchain.PublishMerkleRoot(ctx, epochDate); err != nil {
			log.Printf("warning: on-chain publish failed for %s (settlement complete, retry via admin): %v",
				epochDate.Format("2006-01-02"), err)
		}
	}

	return nil
}

func (svc *SettlementService) isTestnet() bool {
	return svc.RtConfig != nil && svc.RtConfig.IsTestnetMode()
}

func (svc *SettlementService) buildMerkleTree(ctx context.Context, epochDate time.Time, minerFinalRewards map[string]int64) error {
	// Resolve reward recipients: in testnet mode use miner address directly,
	// in mainnet mode query RootNet concurrently for the configured reward recipient.
	testnet := svc.isTestnet()

	type recipientResult struct {
		workerAddr string
		recipient  string
		reward     int64
	}

	var eligible []recipientResult
	for workerAddr, reward := range minerFinalRewards {
		if reward <= 0 {
			continue
		}
		eligible = append(eligible, recipientResult{workerAddr: workerAddr, reward: reward})
	}

	// Parallel RootNet lookups (up to 10 concurrent)
	if !testnet && len(eligible) > 0 {
		sem := make(chan struct{}, 10)
		var mu sync.Mutex
		var wg sync.WaitGroup
		for i := range eligible {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				r, err := svc.RootNet.GetRewardRecipient(ctx, eligible[idx].workerAddr)
				if err != nil {
					log.Printf("warning: RootNet lookup failed for %s, using worker address: %v", eligible[idx].workerAddr, err)
					r = strings.ToLower(eligible[idx].workerAddr)
				}
				mu.Lock()
				eligible[idx].recipient = r
				mu.Unlock()
			}(i)
		}
		wg.Wait()
	} else {
		for i := range eligible {
			eligible[i].recipient = strings.ToLower(eligible[i].workerAddr)
		}
	}

	recipientRewards := make(map[string]int64)
	for _, e := range eligible {
		recipientRewards[e.recipient] += e.reward
		if err := svc.Store.SetWorkerEpochRecipient(ctx, epochDate, e.workerAddr, e.recipient); err != nil {
			return fmt.Errorf("set recipient for %s: %w", e.workerAddr, err)
		}
	}

	var leaves []merkle.Leaf
	type leafInfo struct {
		recipient string
		amount    int64
	}
	var infos []leafInfo

	for recipient, amount := range recipientRewards {
		leaves = append(leaves, merkle.Leaf{
			Address: common.HexToAddress(recipient),
			Amount:  big.NewInt(amount),
		})
		infos = append(infos, leafInfo{recipient: recipient, amount: amount})
	}

	if len(leaves) == 0 {
		return nil // No rewards to distribute
	}

	tree, err := merkle.NewTree(leaves)
	if err != nil {
		return fmt.Errorf("new tree: %w", err)
	}

	root := merkle.HashToHex(tree.Root())
	if err := svc.Store.SetEpochMerkleRoot(ctx, epochDate, root); err != nil {
		return fmt.Errorf("set root: %w", err)
	}

	// Store proofs for each recipient
	for i, leaf := range leaves {
		proof, err := tree.Proof(leaf)
		if err != nil {
			return fmt.Errorf("proof for %s: %w", infos[i].recipient, err)
		}
		leafHash := merkle.HashToHex(merkle.HashLeaf(leaf))

		if err := svc.Store.SaveMerkleProof(ctx, store.MerkleProofRow{
			EpochDate: epochDate,
			Recipient: infos[i].recipient,
			Amount:       infos[i].amount,
			LeafHash:     leafHash,
			Proof:        merkle.ProofToHex(proof),
		}); err != nil {
			return fmt.Errorf("save proof for %s: %w", infos[i].recipient, err)
		}
	}

	return nil
}
