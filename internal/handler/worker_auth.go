package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/awp-core/subnet-benchmark/internal/auth"
	"github.com/awp-core/subnet-benchmark/internal/store"
)

type contextKey string

const workerAddressKey contextKey = "miner_address"

// WorkerAddressFromContext retrieves the authenticated miner address from the context.
func WorkerAddressFromContext(ctx context.Context) string {
	v, _ := ctx.Value(workerAddressKey).(string)
	return v
}

// WorkerAuthConfig is the configuration for the miner auth middleware.
type WorkerAuthConfig struct {
	Store            *store.Store
	TimestampMaxDiff time.Duration // Max allowed timestamp drift (fallback if RtConfig is nil)
	CheckSuspension  bool          // Whether to check suspension
	SignatureOnly    bool          // If true, only verify signature (no DB lookup, no registration)
	RegisterCheck    func(ctx context.Context, address string) (bool, error) // Registration eligibility check
	RtConfig         interface{ GetTimestampMaxDiff() time.Duration }        // Dynamic config (optional)
}

// MinerAuth returns the miner signature authentication middleware.
func WorkerAuth(cfg WorkerAuthConfig) func(http.Handler) http.Handler {
	if cfg.TimestampMaxDiff == 0 {
		cfg.TimestampMaxDiff = 30 * time.Second
	}
	if cfg.RegisterCheck == nil {
		cfg.RegisterCheck = func(ctx context.Context, address string) (bool, error) {
			return true, nil
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Extract headers
			addrHex := r.Header.Get("X-Worker-Address")
			sigHex := r.Header.Get("X-Signature")
			tsStr := r.Header.Get("X-Timestamp")

			if addrHex == "" || sigHex == "" || tsStr == "" {
				writeError(w, http.StatusUnauthorized, "missing auth headers")
				return
			}

			// 2. Validate timestamp
			ts, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid timestamp")
				return
			}
			diff := time.Since(time.Unix(ts, 0))
			if diff < 0 {
				diff = -diff
			}
			maxDiff := cfg.TimestampMaxDiff
			if cfg.RtConfig != nil {
				maxDiff = cfg.RtConfig.GetTimestampMaxDiff()
			}
			if diff > maxDiff {
				writeError(w, http.StatusUnauthorized, "timestamp expired")
				return
			}

			// 3. Read body and compute hash
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, http.StatusBadRequest, "read body failed")
				return
			}
			// Reset body so downstream handlers can read it
			r.Body = io.NopCloser(strings.NewReader(string(body)))

			bodyHash := auth.HashBody(body)
			msg := auth.BuildSignMessage(r.Method, r.URL.Path, tsStr, bodyHash)

			// 4. Verify signature
			recovered, err := auth.VerifySignature(msg, sigHex)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid signature")
				return
			}

			// 5. Compare addresses
			claimedAddr, err := auth.AddressFromHex(addrHex)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid address")
				return
			}
			if recovered != claimedAddr {
				writeError(w, http.StatusUnauthorized, "signature mismatch")
				return
			}

			address := strings.ToLower(recovered.Hex())
			ctx := r.Context()

			if cfg.SignatureOnly {
				// Signature-only mode: skip registration and suspension checks
				ctx = context.WithValue(ctx, workerAddressKey, address)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// 6. Registration check
			miner, err := cfg.Store.GetWorker(ctx, address)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if miner == nil {
				// New miner, check registration eligibility
				ok, err := cfg.RegisterCheck(ctx, address)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal error")
					return
				}
				if !ok {
					writeError(w, http.StatusForbidden, "not registered on AWP RootNet, use AWP skill to register first")
					return
				}
				miner, err = cfg.Store.CreateWorker(ctx, address)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal error")
					return
				}
			}

			// 7. Suspension check (optional)
			if cfg.CheckSuspension && miner.IsSuspended() {
				writeError(w, http.StatusForbidden, fmt.Sprintf("suspended until %v", miner.SuspendedUntil))
				return
			}
			// Auto-unsuspend if expired
			if miner.SuspendedUntil != nil && !miner.IsSuspended() {
				cfg.Store.UnsuspendWorker(ctx, address)
			}

			// Inject miner address into context
			ctx = context.WithValue(ctx, workerAddressKey, address)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// FullMinerAuth returns the full auth middleware with suspension check.
func FullWorkerAuth(s *store.Store) func(http.Handler) http.Handler {
	return WorkerAuth(WorkerAuthConfig{
		Store:           s,
		CheckSuspension: true,
	})
}

// LightMinerAuth returns the auth middleware with registration check only (no suspension check).
func LightWorkerAuth(s *store.Store) func(http.Handler) http.Handler {
	return WorkerAuth(WorkerAuthConfig{
		Store:           s,
		CheckSuspension: false,
	})
}

