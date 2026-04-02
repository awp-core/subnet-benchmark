# Architecture

## Overview

Benchmark Worknet (`benchmarkd`) is a single Go binary HTTP server backed by PostgreSQL. It follows a layered architecture with no external frameworks — only Go standard library + `lib/pq` driver + `go-ethereum` crypto/ethclient.

```
+-------------------------------------------------+
|                  HTTP Server                    |
|              (net/http, ServeMux)               |
+-------------------------------------------------+
|   Middleware: AdminAuth, WorkerAuth             |
|   (Bearer token / Ethereum signature)           |
+-------------------------------------------------+
|                  Handlers                       |
|   BenchmarkSet, Question, Poll, Answer,        |
|   WorkerScores, Claims, Admin, AdminUI,        |
|   Public (stats/leaderboard), Frontend         |
+-------------------------------------------------+
|                  Services                       |
|   QuestionService, PollService,                |
|   AnswerService, ScoringService,               |
|   SettlementService, TimerManager,             |
|   RuntimeConfig, RootNetClient, OnchainService |
+-------------------------------------------------+
|                   Store                         |
|               (database/sql)                    |
+-------------------------------------------------+
|                PostgreSQL                       |
+-------------------------------------------------+
|          On-chain (optional)                    |
|   SubnetManager contract (BSC/EVM)             |
+-------------------------------------------------+
```

## Package Layout

| Package | Responsibility |
|---|---|
| `cmd/benchmarkd` | Entry point, dependency wiring, route registration, graceful shutdown |
| `internal/handler` | HTTP handlers, request/response mapping, middleware, embedded admin UI, public API (stats, leaderboard, questions, assignments), frontend pages (landing, worker dashboard) |
| `internal/service` | Business logic orchestration, runtime config, RootNet client, on-chain publishing |
| `internal/store` | Database access (SQL queries) |
| `internal/model` | Shared data structs and status constants |
| `internal/auth` | Ethereum signature verification utilities |
| `internal/minhash` | MinHash + Jaccard similarity for question deduplication |
| `internal/merkle` | OpenZeppelin-compatible Merkle tree for reward distribution |
| `internal/testutil` | Shared test infrastructure (DB setup, schema reset) |

## Key Design Decisions

### Standard Library HTTP

Uses Go 1.22+ `ServeMux` with method-based routing (`GET /path/{param}`). No router framework needed.

### Concurrency Safety

- **Idempotent scoring**: `ScoreQuestion` uses `WHERE status = 'submitted'` — concurrent calls are safe, only the first succeeds
- **Answer submission**: `SetAssignmentReply` uses `WHERE status = 'assigned'` for idempotency
- **Timer map**: `TimerManager` uses a `sync.Mutex` to protect the in-memory timer map.

### Per-Assignment Timers

Instead of periodic scanning, each assignment gets a dedicated `time.AfterFunc` goroutine:

```
Create assignment -> StartReplyTimer(reply_ddl)
Answer submitted  -> CancelTimer()
Timer fires       -> handleTimeout() -> score 0 + suspend check + try scoring
```

Timed-out assignments release their slot. If a question still needs more answers, new workers can pick it up via the next poll.

Timers are in-memory. On restart, `RecoverOnStartup` scans the database and rebuilds them.

### Stateless Workers

Workers have no status field. The workers table only tracks: `address`, `suspended_until`, `epoch_violations`, `last_poll_at`, `created_at`. Workers simply poll for work — the server picks a random submitted question from the oldest 100 and assigns it. When 5 replies are collected, the question is scored.

### Callback Pattern

Services communicate via function callbacks instead of direct dependencies:

```go
PollService.OnAssignmentCreated = timerMgr.StartReplyTimer
AnswerService.OnAnswerSubmitted = func(asgn) {
    timerMgr.CancelTimer(asgn.ID)
    timerMgr.TryScore(asgn.QuestionID)
}
```

This keeps services decoupled — they don't know about TimerManager directly.

### RuntimeConfig (Dynamic Configuration)

`RuntimeConfig` loads all tunable parameters from the `system_config` database table at startup and after admin updates. It is thread-safe (RWMutex-protected) and provides snapshot methods for each service domain (question, poll, settlement, suspension, auth, network). This allows operators to change parameters (e.g. `settlement.total_reward`, `question.reply_timeout_sec`) at runtime via the admin API without restarting the server.

Configuration keys include network settings (`network.testnet_mode`, `network.rootnet_api_url`, `network.chain_rpc_url`, `network.contract_address`, `network.owner_private_key`, `network.chain_id`) which were previously environment variables.

### RootNet Client

`RootNetClient` communicates with the AWP RootNet API for two purposes:

1. **Registration check** — On first request from an unknown worker, the system calls `GET /api/address/{address}/check` to verify the worker is a registered user or agent on RootNet. Unregistered workers are denied access. In testnet mode, this check is bypassed.
2. **Reward recipient lookup** — During settlement, calls `GET /api/users/{address}` to resolve the worker's reward recipient address. If no custom recipient is set, the worker's own address is used.

### OnchainService

When `network.chain_rpc_url`, `network.contract_address`, and `network.owner_private_key` are all set in `system_config`, `OnchainService` is enabled. After settlement builds the Merkle tree, it publishes the Merkle root to the `SubnetManager` smart contract on-chain by calling `setMerkleRoot(uint32 epoch, bytes32 merkleRoot)`. Skipped in testnet mode.

### Merkle Distribution Flow

Reward distribution follows a Merkle-based claim model:

1. **Settlement** computes each worker's `final_reward` for the epoch.
2. **Recipient resolution** — For each worker with reward > 0, query RootNet for the reward recipient address. In testnet mode, the worker's own address is used. Multiple workers mapping to the same recipient have their rewards aggregated.
3. **Merkle tree construction** — Build an OpenZeppelin-compatible Merkle tree from `(recipient, amount)` leaves using double-hashing: `keccak256(keccak256(abi.encode(address, amount)))`.
4. **Proof storage** — Store the Merkle root and per-recipient proofs in PostgreSQL.
5. **On-chain publishing** (optional) — Submit the root to the `SubnetManager` contract.
6. **Claiming** — Recipients call `SubnetManager.claim(epoch, amount, proof)` on-chain to mint Alpha tokens.
7. **Proof retrieval** — Public API at `GET /api/v1/claims/{address}` provides proofs needed for claiming.

### Admin UI

An embedded HTML page served at `GET /admin/` provides a web dashboard for managing the system. It is compiled into the binary via Go's `embed` package from `internal/handler/static/admin.html`.

### Frontend Pages

Two public-facing pages are embedded into the binary:

- **Landing page** (`GET /`) — Served from `internal/handler/static/index.html`. Displays protocol stats, benchmark sets, recent questions, leaderboard, and scoring information.
- **Worker dashboard** (`GET /app/`) — Served from `internal/handler/static/app.html`. An SPA with MetaMask wallet connection and EIP-191 signing in the browser. Provides tabs for Status, Questions, Assignments, Epochs, Claims, and Leaderboard.

### Auto Settlement Scheduler

On startup, `benchmarkd` launches a background goroutine (`startSettlementScheduler`) that triggers epoch settlement daily at UTC 01:00 for the previous day. This runs in addition to the manual `POST /admin/v1/settle` endpoint. The scheduler is gracefully stopped on shutdown.

### MinHash Deduplication

Questions are fingerprinted using MinHash (128 hash functions, character-level 3-grams, FNV-1a). Signatures are stored as `BYTEA` in PostgreSQL. New submissions are compared against existing questions in the same BenchmarkSet with Jaccard similarity threshold of 0.9 (configurable).

## Database Schema

8 tables in a single migration file (`migrations/001_init.sql`):

- `workers` — Worker addresses, suspension info, violation counts
- `benchmark_sets` — Question categories with requirements and limits
- `questions` — Submitted questions with scores, shares, and MinHash fingerprints
- `assignments` — Answer assignments with deadlines, replies, and scores
- `epochs` — Daily settlement records with Merkle roots
- `worker_epoch_rewards` — Per-worker per-epoch reward breakdowns with recipient mapping
- `merkle_proofs` — Per-recipient Merkle proofs for on-chain claiming
- `system_config` — Runtime configuration key-value store

## Authentication Flow

```
Request -> Extract X-Worker-Address, X-Signature, X-Timestamp
        -> Validate timestamp (within 30s)
        -> SHA256(body) -> build signing message
        -> ecrecover -> compare with claimed address
        -> Check RootNet registration (auto-register if eligible; skip in testnet mode)
        -> Optional: check suspension status
        -> Inject worker address into context
        -> Handler
```
