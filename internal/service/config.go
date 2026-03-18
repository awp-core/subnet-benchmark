package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/store"
)

// RuntimeConfig holds all runtime-configurable parameters, loaded from the system_config table.
// Thread-safe: reads use RLock, writes use Lock.
type RuntimeConfig struct {
	mu    sync.RWMutex
	store *store.Store

	// Question
	RequiredAnswers      int
	ReplyTimeout         time.Duration
	RateLimit            time.Duration
	SimilarityMax        float64
	SimilarityMinScore   int
	AnswerPrompt         string

	// Settlement
	TotalReward       int64
	MinTasks          int
	BenchmarkMinScore float64

	// Suspension
	SuspensionThreshold   int
	BaseSuspensionMinutes int

	// Auth
	TimestampMaxDiff time.Duration

	// Network
	TestnetMode     bool
	RootNetAPIURL   string
	ChainRPCURL     string
	ContractAddress string
	OwnerPrivateKey string
	ChainID         int64
}

func NewRuntimeConfig(s *store.Store) *RuntimeConfig {
	return &RuntimeConfig{store: s}
}

// Load reads all config from the database. Called at startup and after admin updates.
func (c *RuntimeConfig) Load(ctx context.Context) error {
	entries, err := c.store.ListConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	kv := make(map[string]string, len(entries))
	for _, e := range entries {
		kv[e.Key] = e.Value
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.RequiredAnswers = getInt(kv, "question.required_answers", 5)
	c.ReplyTimeout = time.Duration(getInt(kv, "question.reply_timeout_sec", 180)) * time.Second
	c.RateLimit = time.Duration(getInt(kv, "question.rate_limit_sec", 60)) * time.Second
	c.SimilarityMax = getFloat(kv, "question.similarity_max", 0.9)
	c.SimilarityMinScore = getInt(kv, "question.similarity_min_score", 2)
	c.AnswerPrompt = getString(kv, "question.answer_prompt", defaultPrompt)
	c.TotalReward = int64(getInt(kv, "settlement.total_reward", 1000000))
	c.MinTasks = getInt(kv, "settlement.min_tasks", 10)
	c.BenchmarkMinScore = getFloat(kv, "settlement.benchmark_min_score", 0.6)
	c.SuspensionThreshold = getInt(kv, "suspension.score_threshold", 0)
	c.BaseSuspensionMinutes = getInt(kv, "suspension.base_minutes", 10)
	c.TimestampMaxDiff = time.Duration(getInt(kv, "auth.timestamp_max_diff_sec", 30)) * time.Second
	c.TestnetMode = getBool(kv, "network.testnet_mode", false)
	c.RootNetAPIURL = getString(kv, "network.rootnet_api_url", "https://tapi.awp.sh")
	c.ChainRPCURL = getString(kv, "network.chain_rpc_url", "")
	c.ContractAddress = getString(kv, "network.contract_address", "")
	c.OwnerPrivateKey = getString(kv, "network.owner_private_key", "")
	c.ChainID = int64(getInt(kv, "network.chain_id", 56))

	return nil
}

// QuestionConfig returns a snapshot of question-related config.
func (c *RuntimeConfig) QuestionConfig() QuestionConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return QuestionConfig{
		RequiredAnswers:      c.RequiredAnswers,
		RateLimit:            c.RateLimit,
		ReplyTimeout:         c.ReplyTimeout,
		SimilarityMinScore:   c.SimilarityMinScore,
		SimilarityMaxJaccard: c.SimilarityMax,
	}
}

// PollConfig returns a snapshot of poll-related config.
func (c *RuntimeConfig) PollConfig() PollConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return PollConfig{
		ReplyTimeout: c.ReplyTimeout,
	}
}

// SettlementConfig returns a snapshot of settlement-related config.
func (c *RuntimeConfig) SettlementConfig() SettlementConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return SettlementConfig{
		TotalReward:       c.TotalReward,
		MinTasks:          c.MinTasks,
		BenchmarkMinScore: c.BenchmarkMinScore,
	}
}

// GetAnswerPrompt returns the current answer prompt.
func (c *RuntimeConfig) GetAnswerPrompt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AnswerPrompt
}

// GetSuspensionThreshold returns the current suspension score threshold.
func (c *RuntimeConfig) GetSuspensionThreshold() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SuspensionThreshold
}

// GetBaseSuspensionMinutes returns the base suspension duration.
func (c *RuntimeConfig) GetBaseSuspensionMinutes() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BaseSuspensionMinutes
}

// GetTimestampMaxDiff returns the auth timestamp max drift.
func (c *RuntimeConfig) GetTimestampMaxDiff() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TimestampMaxDiff
}

// IsTestnetMode returns whether testnet mode is active.
func (c *RuntimeConfig) IsTestnetMode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TestnetMode
}

// GetOnchainConfig returns a snapshot of on-chain config. Empty ChainRPCURL means disabled.
func (c *RuntimeConfig) GetOnchainConfig() OnchainConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return OnchainConfig{
		RPCURL:          c.ChainRPCURL,
		ContractAddress: c.ContractAddress,
		PrivateKeyHex:   c.OwnerPrivateKey,
		ChainID:         c.ChainID,
	}
}

// GetRootNetAPIURL returns the RootNet API base URL.
func (c *RuntimeConfig) GetRootNetAPIURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.RootNetAPIURL
}

func getBool(kv map[string]string, key string, fallback bool) bool {
	if v, ok := kv[key]; ok {
		return v == "true" || v == "1" || v == "yes"
	}
	return fallback
}

func getInt(kv map[string]string, key string, fallback int) int {
	if v, ok := kv[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getFloat(kv map[string]string, key string, fallback float64) float64 {
	if v, ok := kv[key]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getString(kv map[string]string, key string, fallback string) string {
	if v, ok := kv[key]; ok && v != "" {
		return v
	}
	return fallback
}
