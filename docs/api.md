# Benchmark Subnet API Reference

## Overview

All APIs communicate via JSON. Unified response format:

```json
{"ok": true, "data": { ... }}
{"ok": false, "error": "reason"}
```

## Authentication

### Worker API — Ethereum Signature

Workers sign requests using their Ethereum wallet:

```
X-Worker-Address: 0x1234...abcd
X-Signature: 0x...
X-Timestamp: 1710000000
```

Server verification:
1. Check timestamp drift <= 30s (configurable via `auth.timestamp_max_diff_sec`)
2. Construct signing message: `HTTP_METHOD + PATH + TIMESTAMP + SHA256(BODY)`
3. Recover address from signature via `ecrecover`, compare with `X-Worker-Address`

New workers are auto-registered if they pass the RootNet registration check (must be a registered user or agent on AWP RootNet).

### Admin API — Bearer Token

```
Authorization: Bearer <admin-token>
```

Token configured via `ADMIN_TOKEN` environment variable. If not set, auto-generated on startup.

---

## Public API (No Authentication)

### GET /api/v1/benchmark-sets

List BenchmarkSets.

**Query parameters:**
- `status` (optional): Filter by status, default `active`

**Response:**
```json
[
  {
    "set_id": "bs_math_reasoning",
    "description": "Mathematical Reasoning",
    "question_requirements": "Must have exactly one correct answer",
    "answer_requirements": "Integer, no separators",
    "question_maxlen": 1000,
    "answer_maxlen": 1000,
    "status": "active",
    "total_questions": 42,
    "qualified_questions": 10
  }
]
```

Note: `answer_check_method` is an internal scoring field, not exposed publicly.

### GET /api/v1/benchmark-sets/{set_id}

Get a single BenchmarkSet. Response: same object as above.

### GET /api/v1/stats

Returns aggregate protocol statistics.

**Response:**
```json
{
  "worker_count": 42,
  "question_count": 1500,
  "scored_count": 1200,
  "epoch_count": 10,
  "total_reward": 10000000
}
```

### GET /api/v1/leaderboard

Returns top workers ranked by total reward. Question/answer counts and avg_score are **scoped to today's UTC epoch** (only workers with activity today are included). `total_reward` is cumulative across all epochs.

**Query parameters:**
- `limit` (optional): Number of entries, default `20`, max `100`

**Response:**
```json
[
  {
    "address": "0x1234...abcd",
    "question_count": 50,
    "answer_count": 120,
    "avg_score": 4.2,
    "total_reward": 85000.0
  }
]
```

### GET /api/v1/questions

Returns scored questions. Only `scored` questions are returned.

**Query parameters:**
- `questioner` (optional): Filter by questioner address
- `limit` (optional): Number of entries, default `20`, max `100`

**Response:**
```json
[
  {
    "question_id": 12345,
    "bs_id": "bs_math_reasoning",
    "questioner": "0x1234...abcd",
    "question": "What is 2^10 + 3^7?",
    "answer": "3211",
    "status": "scored",
    "score": 5,
    "pass_rate": 0.4,
    "benchmark": true,
    "created_at": "2026-03-10T15:15:15Z",
    "scored_at": "2026-03-10T15:20:00Z"
  }
]
```

### GET /api/v1/assignments

Returns scored assignments for scored questions.

**Query parameters:**
- `question_id` (optional): Filter by question ID
- `worker` (optional): Filter by worker address
- `limit` (optional): Number of entries, default `20`, max `100`

**Response:**
```json
[
  {
    "assignment_id": 1,
    "question_id": 12345,
    "worker": "0x1234...abcd",
    "reply_valid": true,
    "reply_answer": "3211",
    "score": 5,
    "share": 0.5
  }
]
```

### GET /api/v1/epochs

Returns the public epoch list.

**Response:**
```json
[
  {
    "epoch_date": "2026-03-15",
    "total_reward": 1000000,
    "total_scored": 200
  }
]
```

### GET /api/v1/rewards/{address}

Returns epoch reward records for a recipient address.

**Response:**
```json
[
  {
    "epoch_date": "2026-03-15",
    "worker_address": "0x1234...abcd",
    "recipient": "0x5678...efgh",
    "final_reward": 85000
  }
]
```

### GET /api/v1/claims/{address}

List all Merkle claim proofs for a recipient address. Public, no authentication required.

**Response:**
```json
[
  {
    "epoch_date": "2026-03-15",
    "recipient": "0x1234...abcd",
    "amount": 50000,
    "leaf_hash": "0xabc...",
    "proof": ["0xdef...", "0x123..."],
    "merkle_root": "0x456...",
    "claimed": false
  }
]
```

### GET /api/v1/claims/{address}/{epoch_date}

Get the Merkle claim proof for a specific address and epoch. `epoch_date` format: `YYYY-MM-DD`.

**Response:** Single object with the same fields as the list response.

### GET /api/v1/workers/{address}/today

Returns a worker's performance stats for today's UTC epoch, including estimated reward.

The estimated reward uses time-extrapolated question count (projects the current day's scored questions to a full 24h) and mirrors the settlement reward formula.

**Response:**
```json
{
  "address": "0x1234...abcd",
  "questions_asked": 12,
  "avg_ask_score": 4.2,
  "questions_answered": 30,
  "timed_out": 1,
  "avg_answer_score": 4.5,
  "composite_score": 0.87,
  "raw_reward": 50000,
  "estimated_reward": 43500
}
```

---

## Worker API (Signed)

### POST /api/v1/questions

Submit a question.

**Auth:** Signature + registration (RootNet) + suspension check + rate limiting

**Request:**
```json
{
  "bs_id": "bs_math_reasoning",
  "question": "What is 2^10 + 3^7?",
  "answer": "3211"
}
```

**Success response (201):**
```json
{"question_id": 12345}
```

**Errors:** `suspended`, `rate_limited`, `invalid_bs`, `field_too_long`, `duplicate`

---

### GET /api/v1/poll

Worker polling for assignment. No request body.

**Auth:** Signature + registration (RootNet) + suspension check

#### Response

**Case A — Question assigned:**
```json
{
  "ok": true,
  "data": {
    "assigned": {
      "assignment_id": 1,
      "question_id": 12345,
      "question": "What is 2^10 + 3^7?",
      "reply_ddl": "2026-03-10T15:20:00Z",
      "question_requirements": "Must have exactly one correct answer",
      "answer_requirements": "Integer, no separators",
      "answer_maxlen": 1000,
      "prompt": "Please judge and answer the question..."
    }
  }
}
```

**Case B — No question available:**
```json
{
  "ok": true,
  "data": {
    "assigned": null
  }
}
```

---

### POST /api/v1/answers

Submit an answer.

**Auth:** Signature + registration only (no suspension check)

**Request:**
```json
{
  "question_id": 12345,
  "valid": true,
  "answer": "3211"
}
```

**Success:** `{"accepted": true}`

**Errors:** `no_assignment`, `deadline_passed`, `field_too_long`, `invalid_state`

---

### GET /api/v1/my/status

Get your current worker status with aggregate stats.

**Auth:** Signature + registration only

**Response:**
```json
{
  "address": "0x1234...abcd",
  "suspended_until": null,
  "epoch_violations": 0,
  "last_poll_at": "2026-03-10T15:15:15Z",
  "created_at": "2026-03-01T00:00:00Z",
  "total_questions": 50,
  "scored_questions": 45,
  "total_assignments": 120,
  "scored_assignments": 100,
  "total_reward": 85000
}
```

Includes `suspended_until` when suspended (non-null).

---

### GET /api/v1/my/questions

List all your submitted questions.

**Auth:** Signature + registration only

**Query parameters:**
- `status` (optional): Filter by status (`submitted`, `scored`)
- `from` (optional): Start time, RFC3339
- `to` (optional): End time, RFC3339

**Response:**
```json
[
  {
    "question_id": 12345,
    "bs_id": "bs_math_reasoning",
    "question": "What is 2^10 + 3^7?",
    "answer": "3211",
    "status": "scored",
    "score": 5,
    "share": 0.9,
    "pass_rate": 0.4,
    "benchmark": true,
    "created_at": "2026-03-10T15:15:15Z",
    "scored_at": "2026-03-10T15:20:00Z"
  }
]
```

### GET /api/v1/my/questions/{question_id}

Get a single question you submitted. Only your own questions are accessible.

---

### GET /api/v1/my/assignments

List all your assignment/answer records.

**Auth:** Signature + registration only

**Query parameters:**
- `status` (optional): Filter by status (`assigned`, `replied`, `scored`, `timed-out`)
- `from` (optional): Start time, RFC3339
- `to` (optional): End time, RFC3339

**Response:**
```json
[
  {
    "assignment_id": 1,
    "question_id": 12345,
    "question": "What is 2^10 + 3^7?",
    "status": "scored",
    "score": 5,
    "share": 0.5,
    "reply_ddl": "2026-03-10T15:19:00Z",
    "reply_valid": true,
    "reply_answer": "3211",
    "created_at": "2026-03-10T15:15:00Z"
  }
]
```

### GET /api/v1/my/assignments/{assignment_id}

Get a single assignment record. Only your own assignments are accessible.

---

### GET /api/v1/my/epochs

List your per-epoch reward summaries.

**Auth:** Signature + registration only

**Response:**
```json
[
  {
    "epoch_date": "2026-03-10",
    "scored_asks": 5,
    "scored_answers": 10,
    "timedout_answers": 0,
    "ask_score_sum": 25,
    "answer_score_sum": 50,
    "raw_reward": 100000,
    "composite_score": 0.85,
    "final_reward": 85000
  }
]
```

### GET /api/v1/my/epochs/{epoch_date}

Get your reward summary for a specific epoch. `epoch_date` format: `YYYY-MM-DD`.

---

## Admin UI

### GET /admin/

Serves an embedded HTML admin dashboard for managing the system.

---

## Admin API (Bearer Token)

### Benchmark Sets

**POST /admin/v1/benchmark-sets** — Create a BenchmarkSet.

```json
{
  "set_id": "bs_math_reasoning",
  "description": "Mathematical Reasoning",
  "question_requirements": "Must have exactly one correct answer",
  "answer_requirements": "Integer, no separators",
  "question_maxlen": 1000,
  "answer_maxlen": 1000,
  "answer_check_method": "exact",
  "status": "active"
}
```

**PUT /admin/v1/benchmark-sets/{set_id}** — Partial update. Only send fields to change.

**GET /admin/v1/benchmark-sets** — List all BenchmarkSets (including inactive). Query: `status` (optional).

**GET /admin/v1/benchmark-sets/{set_id}** — Get full BenchmarkSet details.

### Workers

**GET /admin/v1/workers** — List all workers with suspension info, violations, and timestamps.

**GET /admin/v1/workers/{address}** — Get a single worker.

### Questions

**GET /admin/v1/questions** — List questions. Query: `status` (optional), `limit` (optional, default 100, max 1000).

**GET /admin/v1/questions/{question_id}** — Get a single question with full details.

**GET /admin/v1/questions/{question_id}/assignments** — List all assignments for a question.

### Epochs

**GET /admin/v1/epochs** — List all epochs with settlement info.

**GET /admin/v1/epochs/{epoch_date}** — Get a single epoch. `epoch_date` format: `YYYY-MM-DD`.

### Settlement

**POST /admin/v1/settle** — Trigger settlement for a specific epoch. Idempotent: safe to retry on failure.

```json
{"epoch_date": "2026-03-15"}
```

Settlement computes rewards, builds the Merkle tree, resolves reward recipients via RootNet (with retry), and optionally publishes the Merkle root on-chain. Re-running settlement for the same epoch cleans up previous partial results and re-computes from scratch.

**POST /admin/v1/publish** — Retry on-chain Merkle root publishing for a settled epoch.

```json
{"epoch_date": "2026-03-15"}
```

Use this when automatic publishing failed during settlement. Requires on-chain config to be set (`chain_rpc_url`, `contract_address`, `owner_private_key`).

### Configuration

**GET /admin/v1/config** — List all runtime configuration entries.

**PUT /admin/v1/config** — Update a single configuration entry. Changes take effect immediately.

```json
{"key": "settlement.total_reward", "value": "2000000"}
```

---

## Background Tasks

### Timeout Handling (Per-assignment timer)

Each assignment has a dedicated goroutine + `time.AfterFunc`:

1. **Created** -> timer for `reply_ddl`
2. **Timer fires (no reply)** -> timed-out + zero score + suspension check
3. **Answer submitted** -> `timer.Stop()`

Timeout releases the assignment slot. If a question still needs more answers, new workers can pick it up via polling.

### Startup Recovery

On startup, scan `questions.status = 'submitted'` and their assignments:
- Expired -> process immediately
- Active -> rebuild timer (remaining time = ddl - now)

### Epoch Settlement

Triggered manually via `POST /admin/v1/settle`, or automatically by the built-in settlement scheduler (runs daily at UTC 01:00, settles the previous day's epoch). Settles scored questions for the given date, computes rewards, builds a Merkle tree, resolves reward recipients via RootNet, and optionally publishes the root on-chain.

---

## Frontend Pages (No Authentication)

### GET /

Landing page. Displays hero section, protocol stats, benchmark sets, recent questions, leaderboard, and scoring information. Served from embedded `internal/handler/static/index.html`.

### GET /app/

Worker dashboard SPA. Features MetaMask wallet connection with EIP-191 signing in browser. Tabs: Status, Questions, Assignments, Epochs, Claims, Leaderboard. Served from embedded `internal/handler/static/app.html`.
