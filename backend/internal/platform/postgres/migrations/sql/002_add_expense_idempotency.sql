CREATE TABLE idempotency_records (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_fingerprint BYTEA NOT NULL,
    state TEXT NOT NULL,
    transaction_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    CONSTRAINT idempotency_records_pkey PRIMARY KEY (user_id, operation, idempotency_key),
    CONSTRAINT idempotency_records_operation_create_expense CHECK (operation = 'CREATE_EXPENSE'),
    CONSTRAINT idempotency_records_key_length CHECK (octet_length(idempotency_key) BETWEEN 1 AND 128),
    CONSTRAINT idempotency_records_key_visible_ascii CHECK (
        (idempotency_key COLLATE "C") ~ '^[!-~]+$'
    ),
    CONSTRAINT idempotency_records_fingerprint_sha256 CHECK (octet_length(request_fingerprint) = 32),
    CONSTRAINT idempotency_records_state_valid CHECK (state IN ('PENDING', 'COMPLETED')),
    CONSTRAINT idempotency_records_completion_valid CHECK (
        (state = 'PENDING' AND transaction_id IS NULL AND completed_at IS NULL)
        OR
        (state = 'COMPLETED' AND transaction_id IS NOT NULL AND completed_at IS NOT NULL)
    ),
    CONSTRAINT idempotency_records_timestamps_ordered CHECK (
        completed_at IS NULL OR completed_at >= created_at
    ),
    CONSTRAINT idempotency_records_transaction_owner_fkey FOREIGN KEY (transaction_id, user_id)
        REFERENCES transactions(id, user_id) ON DELETE RESTRICT,
    CONSTRAINT idempotency_records_transaction_unique UNIQUE (transaction_id)
);

CREATE INDEX transactions_owner_month_idx
    ON transactions(user_id, occurred_at DESC, id DESC);

---- create above / drop below ----

DROP INDEX transactions_owner_month_idx;
DROP TABLE idempotency_records;
