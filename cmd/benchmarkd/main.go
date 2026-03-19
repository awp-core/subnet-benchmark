package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"github.com/awp-core/subnet-benchmark/internal/handler"
	"github.com/awp-core/subnet-benchmark/internal/model"
	"github.com/awp-core/subnet-benchmark/internal/service"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := envOr("DATABASE_URL", "host=localhost dbname=benchmark sslmode=disable")
	adminToken := envOr("ADMIN_TOKEN", "")
	addr := envOr("LISTEN_ADDR", ":8080")

	if adminToken == "" {
		b := make([]byte, 24)
		rand.Read(b)
		adminToken = hex.EncodeToString(b)
		fmt.Printf("ADMIN_TOKEN not set, generated: %s\n", adminToken)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(20)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	s := store.New(db)

	// Load runtime config from database (includes RootNet URL, chain config, etc.)
	rtConfig := service.NewRuntimeConfig(s)
	if err := rtConfig.Load(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: load config from db: %v (using defaults)\n", err)
	}

	rootNet := service.NewRootNetClient(rtConfig.GetRootNetAPIURL())

	scoringSvc := service.NewScoringService(s)
	scoringSvc.RtConfig = rtConfig

	requiredAnswers := 5
	if rtConfig != nil {
		requiredAnswers = rtConfig.QuestionConfig().RequiredAnswers
	}
	timerMgr := service.NewTimerManager(s, scoringSvc, requiredAnswers)
	timerMgr.RtConfig = rtConfig

	questionSvc := &service.QuestionService{
		Store:    s,
		Config:   service.DefaultQuestionConfig(),
		RtConfig: rtConfig,
	}

	pollSvc := &service.PollService{
		Store:               s,
		Config:              service.DefaultPollConfig(),
		RtConfig:            rtConfig,
		OnAssignmentCreated: timerMgr.StartReplyTimer,
	}

	// scoreWg tracks in-flight async scoring goroutines for graceful shutdown.
	var scoreWg sync.WaitGroup

	answerSvc := &service.AnswerService{
		Store: s,
		OnAnswerSubmitted: func(a *model.Assignment) {
			timerMgr.CancelTimer(a.AssignmentID)
			scoreWg.Add(1)
			go func() {
				defer scoreWg.Done()
				timerMgr.TryScore(context.Background(), a.QuestionID)
			}()
		},
	}

	// On-chain service (from DB config, enabled when chain_rpc_url is non-empty)
	var onchainSvc *service.OnchainService
	onchainCfg := rtConfig.GetOnchainConfig()
	if onchainCfg.RPCURL != "" && onchainCfg.ContractAddress != "" && onchainCfg.PrivateKeyHex != "" {
		onchainSvc = &service.OnchainService{
			Store:  s,
			Config: onchainCfg,
		}
		fmt.Println("on-chain publishing enabled")
	}

	settlementSvc := &service.SettlementService{
		Store:    s,
		Config:   service.DefaultSettlementConfig(),
		RtConfig: rtConfig,
		RootNet:  rootNet,
		Onchain:  onchainSvc,
	}

	bsHandler := &handler.BenchmarkSetHandler{Store: s}
	qHandler := &handler.QuestionHandler{Service: questionSvc}
	pollHandler := &handler.PollHandler{Service: pollSvc}
	ansHandler := &handler.AnswerHandler{Service: answerSvc}
	scoresHandler := &handler.WorkerScoresHandler{Store: s}
	claimsHandler := &handler.ClaimsHandler{Store: s}
	adminHandler := &handler.AdminHandler{Store: s, Settlement: settlementSvc, Onchain: onchainSvc, RtConfig: rtConfig}
	publicHandler := &handler.PublicHandler{Store: s, RtConfig: rtConfig}

	mux := http.NewServeMux()
	registerRoutes(mux, s, rootNet, rtConfig, bsHandler, qHandler, pollHandler, ansHandler, scoresHandler, claimsHandler, adminHandler, publicHandler, adminToken)

	// Startup recovery
	if err := timerMgr.RecoverOnStartup(context.Background()); err != nil {
		return fmt.Errorf("recover timers: %w", err)
	}

	// Auto settlement scheduler
	settlementStop := startSettlementScheduler(settlementSvc)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		fmt.Printf("evod listening on %s\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "listen error: %v\n", err)
		}
	}()

	<-ctx.Done()
	fmt.Println("shutting down...")
	settlementStop()

	// Wait for in-flight scoring goroutines to finish.
	log.Println("waiting for in-flight scoring to complete...")
	scoreWg.Wait()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// startSettlementScheduler runs epoch settlement daily at UTC 01:00.
// Returns a stop function to cancel the scheduler.
func startSettlementScheduler(svc *service.SettlementService) func() {
	done := make(chan struct{})

	go func() {
		for {
			now := time.Now().UTC()
			// Next UTC 01:00
			next := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, time.UTC)
			if !now.Before(next) {
				next = next.Add(24 * time.Hour)
			}
			delay := time.Until(next)
			fmt.Printf("next settlement at %s (in %s)\n", next.Format(time.RFC3339), delay.Round(time.Second))

			select {
			case <-time.After(delay):
				// Settle yesterday
				yesterday := next.Add(-24 * time.Hour)
				epochDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)
				fmt.Printf("auto-settling epoch %s\n", epochDate.Format("2006-01-02"))
				if err := svc.Settle(context.Background(), epochDate); err != nil {
					fmt.Fprintf(os.Stderr, "auto-settlement failed for %s: %v\n", epochDate.Format("2006-01-02"), err)
				} else {
					fmt.Printf("auto-settlement completed for %s\n", epochDate.Format("2006-01-02"))
				}
			case <-done:
				return
			}
		}
	}()

	return func() { close(done) }
}

func registerRoutes(mux *http.ServeMux, s *store.Store,
	rootNet *service.RootNetClient, rtConfig *service.RuntimeConfig,
	bs *handler.BenchmarkSetHandler, q *handler.QuestionHandler,
	poll *handler.PollHandler, ans *handler.AnswerHandler,
	scores *handler.WorkerScoresHandler, claims *handler.ClaimsHandler,
	admin *handler.AdminHandler, pub *handler.PublicHandler,
	adminToken string) {

	// In testnet mode, skip RootNet registration check (allow all miners).
	registerCheck := func(ctx context.Context, addr string) (bool, error) {
		if rtConfig.IsTestnetMode() {
			return true, nil
		}
		return rootNet.IsRegistered(ctx, addr)
	}

	// Public API
	mux.HandleFunc("GET /api/v1/benchmark-sets", bs.HandlePublicList)
	mux.HandleFunc("GET /api/v1/benchmark-sets/{set_id}", bs.HandlePublicGet)
	mux.HandleFunc("GET /api/v1/stats", pub.HandleStats)
	mux.HandleFunc("GET /api/v1/leaderboard", pub.HandleLeaderboard)
	mux.HandleFunc("GET /api/v1/questions", pub.HandlePublicQuestions)
	mux.HandleFunc("GET /api/v1/assignments", pub.HandlePublicAssignments)
	mux.HandleFunc("GET /api/v1/epochs", pub.HandlePublicEpochs)
	mux.HandleFunc("GET /api/v1/rewards/{address}", pub.HandleRecipientRewards)
	mux.HandleFunc("GET /api/v1/workers/{address}/today", pub.HandleWorkerToday)

	// Miner API (full auth)
	fullAuth := handler.WorkerAuth(handler.WorkerAuthConfig{
		Store: s, CheckSuspension: true, RegisterCheck: registerCheck,
	})
	mux.Handle("POST /api/v1/questions", fullAuth(http.HandlerFunc(q.HandleSubmit)))
	mux.Handle("GET /api/v1/poll", fullAuth(http.HandlerFunc(poll.HandlePoll)))

	// Miner API (light auth)
	lightAuth := handler.WorkerAuth(handler.WorkerAuthConfig{
		Store: s, CheckSuspension: false, RegisterCheck: registerCheck,
	})
	mux.Handle("POST /api/v1/answers", lightAuth(http.HandlerFunc(ans.HandleSubmit)))
	mux.Handle("GET /api/v1/my/status", lightAuth(http.HandlerFunc(scores.HandleMyStatus)))
	mux.Handle("GET /api/v1/my/questions", lightAuth(http.HandlerFunc(scores.HandleMyQuestions)))
	mux.Handle("GET /api/v1/my/questions/{question_id}", lightAuth(http.HandlerFunc(scores.HandleMyQuestion)))
	mux.Handle("GET /api/v1/my/assignments", lightAuth(http.HandlerFunc(scores.HandleMyAssignments)))
	mux.Handle("GET /api/v1/my/assignments/{assignment_id}", lightAuth(http.HandlerFunc(scores.HandleMyAssignment)))
	mux.Handle("GET /api/v1/my/epochs", lightAuth(http.HandlerFunc(scores.HandleMyEpochs)))
	mux.Handle("GET /api/v1/my/epochs/{epoch_date}", lightAuth(http.HandlerFunc(scores.HandleMyEpoch)))
	// Claim API (public, no auth — proof is not secret)
	mux.HandleFunc("GET /api/v1/claims/{address}", claims.HandleClaims)
	mux.HandleFunc("GET /api/v1/claims/{address}/{epoch_date}", claims.HandleClaim)

	// Frontend (disabled for now)
	// mux.HandleFunc("GET /{$}", handler.HandleIndex)
	// mux.HandleFunc("GET /app/", handler.HandleApp)

	// Admin API
	adminAuth := handler.AdminAuth(adminToken)
	mux.Handle("POST /admin/v1/benchmark-sets", adminAuth(http.HandlerFunc(bs.HandleAdminCreate)))
	mux.Handle("PUT /admin/v1/benchmark-sets/{set_id}", adminAuth(http.HandlerFunc(bs.HandleAdminUpdate)))
	mux.Handle("GET /admin/v1/benchmark-sets", adminAuth(http.HandlerFunc(bs.HandleAdminList)))
	mux.Handle("GET /admin/v1/benchmark-sets/{set_id}", adminAuth(http.HandlerFunc(bs.HandleAdminGet)))
	mux.Handle("GET /admin/v1/workers", adminAuth(http.HandlerFunc(admin.HandleListWorkers)))
	mux.Handle("GET /admin/v1/workers/{address}", adminAuth(http.HandlerFunc(admin.HandleGetWorker)))
	mux.Handle("GET /admin/v1/questions", adminAuth(http.HandlerFunc(admin.HandleListQuestions)))
	mux.Handle("GET /admin/v1/questions/{question_id}", adminAuth(http.HandlerFunc(admin.HandleGetQuestion)))
	mux.Handle("GET /admin/v1/questions/{question_id}/assignments", adminAuth(http.HandlerFunc(admin.HandleListAssignments)))
	mux.Handle("GET /admin/v1/epochs", adminAuth(http.HandlerFunc(admin.HandleListEpochs)))
	mux.Handle("GET /admin/v1/epochs/{epoch_date}", adminAuth(http.HandlerFunc(admin.HandleGetEpoch)))
	mux.Handle("POST /admin/v1/settle", adminAuth(http.HandlerFunc(admin.HandleTriggerSettlement)))
	mux.Handle("POST /admin/v1/publish", adminAuth(http.HandlerFunc(admin.HandlePublishMerkleRoot)))
	mux.Handle("GET /admin/v1/config", adminAuth(http.HandlerFunc(admin.HandleListConfig)))
	mux.Handle("PUT /admin/v1/config", adminAuth(http.HandlerFunc(admin.HandleUpdateConfig)))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
