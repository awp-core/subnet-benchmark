-- 001_init.sql
-- Benchmark Subnet — complete schema

-- ============================================================
-- 1. workers
-- ============================================================
CREATE TABLE workers (
    address          TEXT PRIMARY KEY,
    suspended_until  TIMESTAMPTZ,
    epoch_violations INT NOT NULL DEFAULT 0,
    last_poll_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- 2. benchmark_sets
-- ============================================================
CREATE TABLE benchmark_sets (
    set_id              TEXT PRIMARY KEY,
    description         TEXT NOT NULL DEFAULT '',
    question_requirements TEXT NOT NULL DEFAULT '',
    answer_requirements TEXT NOT NULL DEFAULT '',
    question_maxlen     INT NOT NULL DEFAULT 1000,
    answer_maxlen       INT NOT NULL DEFAULT 1000,
    answer_check_method TEXT NOT NULL DEFAULT 'exact',
    status              TEXT NOT NULL DEFAULT 'active',
    total_questions     INT NOT NULL DEFAULT 0,
    qualified_questions INT NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT bs_status_check CHECK (status IN ('active','inactive')),
    CONSTRAINT bs_check_method_check CHECK (answer_check_method IN ('exact'))
);

-- ============================================================
-- 3. questions
-- ============================================================
CREATE TABLE questions (
    question_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bs_id       TEXT NOT NULL REFERENCES benchmark_sets(set_id),
    questioner  TEXT NOT NULL REFERENCES workers(address),
    question    TEXT NOT NULL,
    answer      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'submitted',
    share       NUMERIC(5,4) NOT NULL DEFAULT 0,
    score       INT NOT NULL DEFAULT 0,
    pass_rate   NUMERIC(5,4) NOT NULL DEFAULT 0,
    benchmark   BOOLEAN NOT NULL DEFAULT FALSE,
    minhash     BYTEA,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    scored_at   TIMESTAMPTZ,
    CONSTRAINT questions_status_check CHECK (status IN ('submitted','scored'))
);
CREATE INDEX idx_questions_submitted ON questions (created_at) WHERE status = 'submitted';
CREATE INDEX idx_questions_scored_at ON questions (scored_at) WHERE status = 'scored';
CREATE INDEX idx_questions_bs_score ON questions (bs_id, score) WHERE score >= 2;
CREATE INDEX idx_questions_questioner ON questions (questioner);
CREATE INDEX idx_questions_scored_questioner ON questions (questioner) WHERE status = 'scored';
CREATE INDEX idx_questions_scored_at_range ON questions (scored_at) WHERE status = 'scored' AND scored_at IS NOT NULL;

-- ============================================================
-- 4. assignments
-- ============================================================
CREATE TABLE assignments (
    assignment_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    question_id   BIGINT NOT NULL REFERENCES questions(question_id),
    worker        TEXT NOT NULL REFERENCES workers(address),
    status        TEXT NOT NULL DEFAULT 'claimed',
    reply_ddl     TIMESTAMPTZ NOT NULL,
    reply_valid   BOOLEAN,
    reply_answer  TEXT,
    replied_at    TIMESTAMPTZ,
    share         NUMERIC(5,4) NOT NULL DEFAULT 0,
    score         INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT assignments_status_check CHECK (
        status IN ('claimed','replied','scored','timed-out')
    )
);
CREATE INDEX idx_assignments_reply_ddl ON assignments (reply_ddl) WHERE status = 'claimed';
CREATE INDEX idx_assignments_active ON assignments (question_id) WHERE status IN ('claimed', 'replied');
CREATE UNIQUE INDEX idx_assignments_one_per_worker ON assignments (worker) WHERE status = 'claimed';
CREATE INDEX idx_assignments_question ON assignments (question_id);
CREATE INDEX idx_assignments_worker ON assignments (worker);
CREATE INDEX idx_assignments_scored_worker ON assignments (worker) WHERE status = 'scored';
CREATE INDEX idx_assignments_scored_question ON assignments (question_id) WHERE status IN ('scored', 'timed-out');

-- ============================================================
-- 5. epochs
-- ============================================================
CREATE TABLE epochs (
    epoch_date    DATE PRIMARY KEY,
    total_reward  BIGINT NOT NULL DEFAULT 1000000,
    total_scored  INT NOT NULL DEFAULT 0,
    settled_at    TIMESTAMPTZ,
    merkle_root   TEXT,
    published_at  TIMESTAMPTZ
);

-- ============================================================
-- 6. worker_epoch_rewards
-- ============================================================
CREATE TABLE worker_epoch_rewards (
    epoch_date      DATE NOT NULL REFERENCES epochs(epoch_date),
    worker_address   TEXT NOT NULL REFERENCES workers(address),
    recipient       TEXT,
    scored_asks     INT NOT NULL DEFAULT 0,
    scored_answers  INT NOT NULL DEFAULT 0,
    timedout_answers INT NOT NULL DEFAULT 0,
    ask_score_sum   INT NOT NULL DEFAULT 0,
    answer_score_sum INT NOT NULL DEFAULT 0,
    raw_reward      BIGINT NOT NULL DEFAULT 0,
    composite_score NUMERIC(5,4) NOT NULL DEFAULT 0,
    final_reward    BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (epoch_date, worker_address)
);
CREATE INDEX idx_worker_epoch_rewards_recipient ON worker_epoch_rewards (recipient);

-- ============================================================
-- 7. merkle_proofs
-- ============================================================
CREATE TABLE merkle_proofs (
    epoch_date    DATE NOT NULL REFERENCES epochs(epoch_date),
    recipient     TEXT NOT NULL,
    amount        BIGINT NOT NULL,
    leaf_hash     TEXT NOT NULL,
    proof         TEXT[] NOT NULL,
    claimed       BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (epoch_date, recipient)
);
CREATE INDEX idx_merkle_proofs_recipient ON merkle_proofs (recipient);

-- ============================================================
-- 8. system_config
-- ============================================================
CREATE TABLE system_config (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO system_config (key, value, description) VALUES
    ('question.required_answers', '5', 'Number of answers required per question'),
    ('question.reply_timeout_sec', '180', 'Answer reply timeout in seconds'),
    ('question.rate_limit_sec', '60', 'Min seconds between question submissions per worker'),
    ('question.similarity_max', '0.85', 'Max Jaccard similarity (>= means duplicate)'),
    ('question.similarity_min_score', '2', 'Min question score for similarity check'),
    ('question.answer_prompt', 'Please judge and answer the question. First judge whether it''s valid or not. A valid question should be answerable and meet all the question_requirements that I sent to you. Answer the question with accordance to answer_requirements and answer_maxlen that I sent to you if you think the question is valid.', 'Default prompt sent to answerers'),
    ('settlement.total_reward', '1000000', 'Total reward pool per epoch'),
    ('settlement.min_tasks', '10', 'Min scored tasks for final reward eligibility'),
    ('settlement.benchmark_min_score', '0.6', 'Min composite score for benchmark questioner'),
    ('suspension.score_threshold', '0', 'Score < this triggers suspension (0 = disabled)'),
    ('suspension.base_minutes', '10', 'First offense suspension duration in minutes'),
    ('auth.timestamp_max_diff_sec', '30', 'Max allowed timestamp drift in seconds'),
    ('network.testnet_mode', 'false', 'Testnet mode: skip RootNet check, use worker address as recipient, skip on-chain publishing'),
    ('network.rootnet_api_url', 'https://tapi.awp.sh', 'AWP RootNet API base URL'),
    ('network.chain_rpc_url', '', 'EVM chain RPC endpoint (empty = on-chain publishing disabled)'),
    ('network.contract_address', '', 'SubnetManager contract address'),
    ('network.owner_private_key', '', 'Backend wallet private key with MERKLE_ROLE (hex, no 0x prefix)'),
    ('network.chain_id', '56', 'EVM chain ID');
