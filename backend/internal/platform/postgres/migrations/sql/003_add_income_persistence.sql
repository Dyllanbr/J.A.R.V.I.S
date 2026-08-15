ALTER TABLE audit_events
    DROP CONSTRAINT audit_events_transaction_owner_fkey;

ALTER TABLE idempotency_records
    DROP CONSTRAINT idempotency_records_transaction_owner_fkey;

ALTER TABLE transactions
    DROP CONSTRAINT transactions_type_expense,
    DROP CONSTRAINT transactions_payment_method_valid,
    ALTER COLUMN payment_method DROP NOT NULL,
    ADD CONSTRAINT transactions_type_valid CHECK (type IN ('EXPENSE', 'INCOME')),
    ADD CONSTRAINT transactions_payment_method_by_type CHECK (
        (
            type = 'EXPENSE'
            AND payment_method IS NOT NULL
            AND payment_method IN ('PIX', 'DEBIT', 'CREDIT', 'CASH')
        )
        OR
        (type = 'INCOME' AND payment_method IS NULL)
    ),
    ADD CONSTRAINT transactions_id_user_id_type_unique UNIQUE (id, user_id, type);

ALTER TABLE audit_events
    DROP CONSTRAINT audit_events_aggregate_type_expense,
    DROP CONSTRAINT audit_events_event_type_recorded,
    ADD CONSTRAINT audit_events_aggregate_type_valid CHECK (
        aggregate_type IN ('EXPENSE', 'INCOME')
    ),
    ADD CONSTRAINT audit_events_event_matches_aggregate CHECK (
        (aggregate_type = 'EXPENSE' AND event_type = 'EXPENSE_RECORDED')
        OR
        (aggregate_type = 'INCOME' AND event_type = 'INCOME_RECORDED')
    ),
    ADD CONSTRAINT audit_events_transaction_owner_type_fkey
        FOREIGN KEY (aggregate_id, user_id, aggregate_type)
        REFERENCES transactions(id, user_id, type) ON DELETE RESTRICT;

ALTER TABLE idempotency_records
    DROP CONSTRAINT idempotency_records_operation_create_expense,
    ADD CONSTRAINT idempotency_records_operation_valid CHECK (
        operation IN ('CREATE_EXPENSE', 'CREATE_INCOME')
    ),
    ADD COLUMN transaction_type TEXT GENERATED ALWAYS AS (
        CASE operation
            WHEN 'CREATE_EXPENSE' THEN 'EXPENSE'
            WHEN 'CREATE_INCOME' THEN 'INCOME'
        END
    ) STORED,
    ADD CONSTRAINT idempotency_records_transaction_owner_type_fkey
        FOREIGN KEY (transaction_id, user_id, transaction_type)
        REFERENCES transactions(id, user_id, type) ON DELETE RESTRICT;

---- create above / drop below ----

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM transactions WHERE type <> 'EXPENSE')
        OR EXISTS (
            SELECT 1
            FROM audit_events
            WHERE aggregate_type <> 'EXPENSE' OR event_type <> 'EXPENSE_RECORDED'
        )
        OR EXISTS (
            SELECT 1
            FROM idempotency_records
            WHERE operation <> 'CREATE_EXPENSE'
        )
    THEN
        RAISE EXCEPTION 'migration 003 cannot be rolled back while income records exist';
    END IF;
END
$$;

ALTER TABLE audit_events
    DROP CONSTRAINT audit_events_transaction_owner_type_fkey;

ALTER TABLE idempotency_records
    DROP CONSTRAINT idempotency_records_transaction_owner_type_fkey,
    DROP CONSTRAINT idempotency_records_operation_valid,
    DROP COLUMN transaction_type,
    ADD CONSTRAINT idempotency_records_operation_create_expense CHECK (
        operation = 'CREATE_EXPENSE'
    ),
    ADD CONSTRAINT idempotency_records_transaction_owner_fkey
        FOREIGN KEY (transaction_id, user_id)
        REFERENCES transactions(id, user_id) ON DELETE RESTRICT;

ALTER TABLE audit_events
    DROP CONSTRAINT audit_events_aggregate_type_valid,
    DROP CONSTRAINT audit_events_event_matches_aggregate,
    ADD CONSTRAINT audit_events_aggregate_type_expense CHECK (aggregate_type = 'EXPENSE'),
    ADD CONSTRAINT audit_events_event_type_recorded CHECK (event_type = 'EXPENSE_RECORDED'),
    ADD CONSTRAINT audit_events_transaction_owner_fkey
        FOREIGN KEY (aggregate_id, user_id)
        REFERENCES transactions(id, user_id) ON DELETE RESTRICT;

ALTER TABLE transactions
    DROP CONSTRAINT transactions_id_user_id_type_unique,
    DROP CONSTRAINT transactions_type_valid,
    DROP CONSTRAINT transactions_payment_method_by_type,
    ALTER COLUMN payment_method SET NOT NULL,
    ADD CONSTRAINT transactions_type_expense CHECK (type = 'EXPENSE'),
    ADD CONSTRAINT transactions_payment_method_valid CHECK (
        payment_method IN ('PIX', 'DEBIT', 'CREDIT', 'CASH')
    );
