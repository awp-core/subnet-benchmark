# Deployment Guide

## Requirements

- **Go** >= 1.24 (build only)
- **PostgreSQL** >= 14
- A secret token for admin API authentication (optional — auto-generated if not set)

## Build

```bash
make          # Produces bin/benchmarkd
```

Or directly:

```bash
go build -o bin/benchmarkd ./cmd/benchmarkd
```

## Database Setup

```bash
# Create database
createdb benchmark

# Apply migration
psql -d benchmark -f migrations/001_init.sql
```

The single migration file creates all tables (`workers`, `benchmark_sets`, `questions`, `assignments`, `epochs`, `worker_epoch_rewards`, `merkle_proofs`, `system_config`) and inserts default runtime configuration entries.

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | No | `host=localhost dbname=benchmark sslmode=disable` | PostgreSQL connection string |
| `ADMIN_TOKEN` | No | Auto-generated | Bearer token for admin API endpoints |
| `LISTEN_ADDR` | No | `:8080` | HTTP listen address and port |

If `ADMIN_TOKEN` is not set, a random 48-character hex token is generated and printed to stdout on startup.

### Runtime Configuration (Database)

Network, chain, and all tunable parameters are managed via the `system_config` database table, accessible through the Admin UI (Config tab) or the admin API. Changes take effect immediately without restarting the server.

**Network & on-chain settings:**
- `network.testnet_mode` (`false`) — Skip RootNet registration check and on-chain publishing
- `network.rootnet_api_url` (`https://api.awp.network`) — AWP RootNet API base URL
- `network.chain_rpc_url` (empty) — EVM chain RPC endpoint; enables on-chain publishing when set
- `network.contract_address` (empty) — SubnetManager contract address
- `network.owner_private_key` (empty) — Contract owner private key (hex, no 0x prefix)
- `network.chain_id` (`56`) — EVM chain ID (BSC mainnet)

**Question settings:**
- `question.required_answers` (`5`)
- `question.reply_timeout_sec` (`180`)
- `question.rate_limit_sec` (`60`)
- `question.similarity_max` (`0.9`)
- `question.similarity_min_score` (`2`)
- `question.answer_prompt` — LLM prompt sent to answerers

**Settlement settings:**
- `settlement.total_reward` (`1000000`) — Daily reward pool
- `settlement.min_tasks` (`10`) — Minimum scored tasks for non-zero final reward
- `settlement.benchmark_min_score` (`0.6`) — Min composite score for benchmark qualification

**Suspension settings:**
- `suspension.score_threshold` (`0` = disabled) — Score below this triggers suspension
- `suspension.base_minutes` (`10`) — First offense suspension duration

**Auth settings:**
- `auth.timestamp_max_diff_sec` (`30`) — Max allowed clock drift for signature timestamps

### On-chain Configuration

To enable automatic Merkle root publishing to the SubnetManager contract after settlement, set all three on-chain config values via the admin API:

```bash
curl -s -X PUT http://localhost:8080/admin/v1/config \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"key": "network.chain_rpc_url", "value": "https://bsc-dataseed.binance.org"}'

curl -s -X PUT http://localhost:8080/admin/v1/config \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"key": "network.contract_address", "value": "0x..."}'

curl -s -X PUT http://localhost:8080/admin/v1/config \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"key": "network.owner_private_key", "value": "abc123..."}'
```

If any of these are empty, on-chain publishing is skipped and settlement still works (proofs are stored in the database only). On-chain publishing is also skipped in testnet mode.

### Database URL Examples

```bash
# Local, peer auth
DATABASE_URL="host=localhost dbname=benchmark sslmode=disable"

# With password
DATABASE_URL="host=localhost dbname=benchmark user=benchmark password=secret sslmode=disable"

# Remote
DATABASE_URL="host=db.example.com dbname=benchmark user=benchmark password=secret sslmode=require"
```

## Running

```bash
export DATABASE_URL="host=localhost dbname=benchmark user=benchmark password=secret sslmode=disable"
export ADMIN_TOKEN="your-secret-admin-token"
export LISTEN_ADDR=":8080"

./bin/benchmarkd
```

Output:
```
evod listening on :8080
next settlement at 2026-03-19T01:00:00Z (in 8h30m)
```

If on-chain config is set:
```
on-chain publishing enabled
```

The server handles `SIGINT` (Ctrl+C) for graceful shutdown with a 5-second timeout.

## Startup Behavior

On startup, `benchmarkd` performs:

1. Connect to PostgreSQL and verify connectivity
2. Auto-generate `ADMIN_TOKEN` if not provided
3. Load runtime configuration from `system_config` table (falls back to defaults on error)
4. Initialize RootNet client for registration checks and reward recipient lookup
5. Initialize OnchainService if chain config is provided
6. **Recover timers**: Scan all `submitted` questions and their assignments, rebuild in-memory timers for active assignments, immediately process expired ones
7. **Start settlement scheduler**: Background goroutine that triggers epoch settlement daily at UTC 01:00 for the previous day
8. Start HTTP server

## Initial Setup

After starting the server, create at least one BenchmarkSet for workers to use:

```bash
curl -s -X POST http://localhost:8080/admin/v1/benchmark-sets \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "set_id": "bs_math",
    "description": "Mathematical Reasoning",
    "question_requirements": "Must have exactly one correct answer. No random or ambiguous questions.",
    "answer_requirements": "Integer values only. No separators. No decimal points for whole numbers.",
    "question_maxlen": 1000,
    "answer_maxlen": 100
  }' | jq .
```

## Health Check

```bash
# Public endpoint, no auth needed
curl -s http://localhost:8080/api/v1/stats | jq .
```

## Frontend Pages

The server embeds three HTML pages:

- **Landing page** at `http://localhost:8080/` — Protocol stats, benchmark sets, recent questions, leaderboard, and scoring information.
- **Worker dashboard** at `http://localhost:8080/app/` — SPA with MetaMask wallet connection and EIP-191 signing. Tabs: Status, Questions, Assignments, Epochs, Claims, Leaderboard.
- **Admin dashboard** at `http://localhost:8080/admin/` — System management UI with config editing, settlement trigger, and data browsing.

## Epoch Settlement

Settlement runs automatically daily at UTC 01:00 for the previous day's epoch. It can also be triggered manually via the admin API:

```bash
curl -s -X POST http://localhost:8080/admin/v1/settle \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"epoch_date": "2026-03-15"}' | jq .
```

Settlement computes rewards, builds a Merkle tree, resolves reward recipients via RootNet, and optionally publishes the root on-chain.

## Monitoring

The server logs to stdout/stderr. Key log patterns:

- `evod listening on :8080` — successful startup
- `ADMIN_TOKEN not set, generated: ...` — auto-generated admin token
- `on-chain publishing enabled` — chain config detected
- `next settlement at ...` — auto settlement scheduler started
- `auto-settling epoch ...` / `auto-settlement completed for ...` — scheduled settlement running
- `warning: load config from db:` — config table not yet initialized
- `timer: ...` — timeout handler events (scoring)
- `shutting down...` — graceful shutdown initiated

## Test Database

For running tests:

```bash
createdb benchmark_test
export BENCHMARK_TEST_DSN="host=localhost dbname=benchmark_test user=ubuntu password=evo_test sslmode=disable"
make test
```

Tests use `-p 1` (sequential packages) to avoid concurrent schema operations on the shared test database. Test infrastructure is in `internal/testutil/`.
