-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Wallets table
CREATE TABLE wallets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    player_id UUID NOT NULL,
    currency VARCHAR(3) NOT NULL,
    balance_cents BIGINT NOT NULL CHECK (balance_cents >= 0),
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_wallets_player_currency UNIQUE (player_id, currency)
);

CREATE INDEX idx_wallets_player ON wallets(player_id);

-- Wager transactions table
CREATE TABLE wager_transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    provider_id VARCHAR(100) NOT NULL,
    external_transaction_id VARCHAR(255) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    player_id UUID NOT NULL,
    round_id VARCHAR(100) NOT NULL,
    game_id VARCHAR(100) NOT NULL,
    kind VARCHAR(20) NOT NULL CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    amount_cents BIGINT NOT NULL CHECK (amount_cents >= 0),
    currency VARCHAR(3) NOT NULL,
    reference_external_transaction_id VARCHAR(255),
    resolved_reference_id UUID REFERENCES wager_transactions(id),
    status VARCHAR(30) NOT NULL CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),
    failure_code VARCHAR(50),
    balance_snapshot_cents BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_wager_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT uq_wager_provider_external_id UNIQUE (provider_id, external_transaction_id)
);

CREATE INDEX idx_wager_pending_ref ON wager_transactions(status) WHERE status = 'PENDING_REFERENCE';
CREATE INDEX idx_wager_provider_ref ON wager_transactions(provider_id, reference_external_transaction_id);

-- Wallet ledger table (Append-only & immutable)
CREATE TABLE wallet_ledger (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    transaction_id UUID NOT NULL REFERENCES wager_transactions(id),
    direction VARCHAR(10) NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    currency VARCHAR(3) NOT NULL,
    balance_before_cents BIGINT NOT NULL CHECK (balance_before_cents >= 0),
    balance_after_cents BIGINT NOT NULL CHECK (balance_after_cents >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_ledger_wallet_transaction UNIQUE (wallet_id, transaction_id)
);

CREATE INDEX idx_ledger_wallet_created ON wallet_ledger(wallet_id, created_at ASC);

-- Enforce Ledger Immutability via Trigger
CREATE OR REPLACE FUNCTION prevent_ledger_modification()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Ledger entries are strictly immutable and cannot be updated or deleted';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_prevent_ledger_modification
BEFORE UPDATE OR DELETE ON wallet_ledger
FOR EACH ROW EXECUTE FUNCTION prevent_ledger_modification();

-- Inbox table for idempotent message consumer
CREATE TABLE inbox (
    consumer_name VARCHAR(100) NOT NULL,
    message_id VARCHAR(255) NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (consumer_name, message_id)
);

-- Outbox table for reliable distributed event publishing
CREATE TABLE outbox (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL UNIQUE,
    aggregate_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PUBLISHED', 'FAILED')),
    retry_count INT NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_outbox_pending_dispatch ON outbox(status, next_retry_at) WHERE status = 'PENDING';
