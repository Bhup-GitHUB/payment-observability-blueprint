CREATE TABLE IF NOT EXISTS ledger_entries (
    id             BIGSERIAL    PRIMARY KEY,
    payment_id     TEXT         NOT NULL UNIQUE,
    merchant_id    TEXT         NOT NULL,
    amount         BIGINT       NOT NULL,
    currency       CHAR(3)      NOT NULL,
    bank_ref       TEXT         NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_entries_merchant_id ON ledger_entries (merchant_id);
CREATE INDEX idx_ledger_entries_created_at  ON ledger_entries (created_at DESC);
