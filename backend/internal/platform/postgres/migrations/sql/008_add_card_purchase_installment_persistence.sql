ALTER TABLE transactions
    ADD COLUMN credit_card_id TEXT,
    ADD COLUMN statement_due_on DATE;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_credit_card_linked_together CHECK (
        (credit_card_id IS NULL AND statement_due_on IS NULL)
        OR (credit_card_id IS NOT NULL AND statement_due_on IS NOT NULL)
    ),
    ADD CONSTRAINT transactions_credit_card_payment_method CHECK (
        credit_card_id IS NULL OR (payment_method IS NOT NULL AND payment_method = 'CREDIT')
    ),
    ADD CONSTRAINT transactions_credit_card_id_valid CHECK (
        credit_card_id IS NULL OR credit_card_id COLLATE "C" ~ '^card_[0-9a-f]{32}$'
    ),
    ADD CONSTRAINT transactions_statement_due_on_supported CHECK (
        statement_due_on IS NULL OR statement_due_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
    ),
    ADD CONSTRAINT transactions_credit_card_owner_fkey FOREIGN KEY (credit_card_id, user_id)
        REFERENCES credit_cards(id, user_id) ON DELETE RESTRICT;

CREATE TABLE installment_plans (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    credit_card_id TEXT NOT NULL,
    expense_id TEXT NOT NULL,
    total_minor BIGINT NOT NULL,
    total_currency TEXT NOT NULL,
    installment_count SMALLINT NOT NULL,
    first_due_on DATE NOT NULL,
    due_day SMALLINT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL,
    cancelled_on DATE,
    CONSTRAINT installment_plans_id_valid CHECK (id COLLATE "C" ~ '^ipl_[0-9a-f]{32}$'),
    CONSTRAINT installment_plans_id_user_unique UNIQUE (id, user_id),
    CONSTRAINT installment_plans_card_owner_fkey FOREIGN KEY (credit_card_id, user_id)
        REFERENCES credit_cards(id, user_id) ON DELETE RESTRICT,
    CONSTRAINT installment_plans_expense_owner_fkey FOREIGN KEY (expense_id, user_id)
        REFERENCES transactions(id, user_id) ON DELETE RESTRICT,
    CONSTRAINT installment_plans_expense_unique UNIQUE (expense_id),
    CONSTRAINT installment_plans_total_positive CHECK (total_minor > 0),
    CONSTRAINT installment_plans_currency_brl CHECK (total_currency = 'BRL'),
    CONSTRAINT installment_plans_count_valid CHECK (installment_count BETWEEN 2 AND 120),
    CONSTRAINT installment_plans_total_covers_count CHECK (total_minor >= installment_count),
    CONSTRAINT installment_plans_due_day_valid CHECK (due_day BETWEEN 1 AND 31),
    CONSTRAINT installment_plans_first_due_on_supported CHECK (
        first_due_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
    ),
    CONSTRAINT installment_plans_status_valid CHECK (status IN ('ACTIVE', 'CANCELLED')),
    CONSTRAINT installment_plans_lifecycle_valid CHECK (
        (status = 'ACTIVE' AND cancelled_on IS NULL)
        OR (status = 'CANCELLED' AND cancelled_on IS NOT NULL
            AND cancelled_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31')
    )
);

CREATE INDEX installment_plans_user_due_idx
    ON installment_plans(user_id, first_due_on ASC, created_at ASC, id ASC);

CREATE TABLE installment_plan_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    installment_plan_id TEXT NOT NULL,
    credit_card_id TEXT NOT NULL,
    expense_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL,
    CONSTRAINT installment_plan_audit_owner_fkey FOREIGN KEY (installment_plan_id, user_id)
        REFERENCES installment_plans(id, user_id) ON DELETE RESTRICT,
    CONSTRAINT installment_plan_audit_card_owner_fkey FOREIGN KEY (credit_card_id, user_id)
        REFERENCES credit_cards(id, user_id) ON DELETE RESTRICT,
    CONSTRAINT installment_plan_audit_expense_owner_fkey FOREIGN KEY (expense_id, user_id)
        REFERENCES transactions(id, user_id) ON DELETE RESTRICT,
    CONSTRAINT installment_plan_audit_event_valid CHECK (
        event_type IN ('INSTALLMENT_PLAN_CREATED', 'INSTALLMENT_PLAN_CANCELLED')
    ),
    CONSTRAINT installment_plan_audit_event_unique UNIQUE (installment_plan_id, event_type)
);

CREATE TABLE card_purchase_idempotency_records (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_fingerprint BYTEA NOT NULL,
    state TEXT NOT NULL,
    expense_id TEXT,
    expense_user_id TEXT,
    expense_description TEXT,
    expense_amount_minor BIGINT,
    expense_currency TEXT,
    expense_payment_method TEXT,
    expense_category_id TEXT,
    expense_credit_card_id TEXT,
    expense_statement_due_on DATE,
    expense_occurred_at TIMESTAMPTZ(6),
    expense_financial_timezone TEXT,
    expense_origin TEXT,
    expense_status TEXT,
    expense_version BIGINT,
    expense_created_at TIMESTAMPTZ(6),
    expense_updated_at TIMESTAMPTZ(6),
    plan_id TEXT,
    plan_user_id TEXT,
    plan_credit_card_id TEXT,
    plan_expense_id TEXT,
    plan_total_minor BIGINT,
    plan_total_currency TEXT,
    plan_installment_count SMALLINT,
    plan_first_due_on DATE,
    plan_due_day SMALLINT,
    plan_status TEXT,
    plan_created_at TIMESTAMPTZ(6),
    plan_cancelled_on DATE,
    created_at TIMESTAMPTZ(6) NOT NULL,
    completed_at TIMESTAMPTZ(6),
    CONSTRAINT card_purchase_idem_pkey PRIMARY KEY (user_id, operation, idempotency_key),
    CONSTRAINT card_purchase_idem_expense_owner_fkey FOREIGN KEY (expense_user_id)
        REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT card_purchase_idem_plan_owner_fkey FOREIGN KEY (plan_user_id)
        REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT card_purchase_idem_operation_valid CHECK (operation = 'CREATE_CARD_PURCHASE'),
    CONSTRAINT card_purchase_idem_key_valid CHECK (
        octet_length(idempotency_key) BETWEEN 1 AND 128
        AND idempotency_key COLLATE "C" ~ '^[!-~]+$'
    ),
    CONSTRAINT card_purchase_idem_fingerprint_valid CHECK (octet_length(request_fingerprint) = 32),
    CONSTRAINT card_purchase_idem_state_valid CHECK (state IN ('PENDING', 'COMPLETED')),
    CONSTRAINT card_purchase_idem_snapshot_valid CHECK (
        (state = 'PENDING' AND expense_id IS NULL AND expense_user_id IS NULL AND plan_id IS NULL AND plan_user_id IS NULL AND completed_at IS NULL
         AND num_nonnulls(
             expense_user_id, expense_description, expense_amount_minor, expense_currency,
             expense_payment_method, expense_category_id, expense_credit_card_id,
             expense_statement_due_on, expense_occurred_at, expense_financial_timezone,
             expense_origin, expense_status, expense_version, expense_created_at,
             expense_updated_at, plan_user_id, plan_credit_card_id, plan_expense_id, plan_total_minor,
             plan_total_currency, plan_installment_count, plan_first_due_on,
             plan_due_day, plan_status, plan_created_at, plan_cancelled_on
         ) = 0)
        OR
        (state = 'COMPLETED'
         AND expense_id IS NOT NULL
         AND expense_user_id IS NOT NULL AND expense_user_id = user_id
         AND expense_description IS NOT NULL
         AND expense_amount_minor IS NOT NULL AND expense_amount_minor > 0
         AND expense_currency IS NOT NULL AND expense_currency = 'BRL'
         AND expense_payment_method IS NOT NULL AND expense_payment_method = 'CREDIT'
         AND expense_credit_card_id IS NOT NULL
         AND expense_credit_card_id COLLATE "C" ~ '^card_[0-9a-f]{32}$'
         AND expense_statement_due_on IS NOT NULL
         AND expense_statement_due_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
         AND expense_occurred_at IS NOT NULL
         AND expense_financial_timezone IS NOT NULL
         AND expense_origin IS NOT NULL
         AND expense_status IS NOT NULL AND expense_status = 'RECORDED'
         AND expense_version IS NOT NULL AND expense_version = 1
         AND expense_created_at IS NOT NULL
         AND expense_updated_at IS NOT NULL
         AND completed_at IS NOT NULL
         AND ((plan_id IS NULL AND plan_user_id IS NULL AND plan_credit_card_id IS NULL AND plan_expense_id IS NULL
               AND plan_total_minor IS NULL AND plan_total_currency IS NULL
               AND plan_installment_count IS NULL AND plan_first_due_on IS NULL
               AND plan_due_day IS NULL AND plan_status IS NULL AND plan_created_at IS NULL
               AND plan_cancelled_on IS NULL)
              OR
              (plan_id IS NOT NULL AND plan_user_id IS NOT NULL AND plan_user_id = user_id
               AND plan_user_id = expense_user_id
               AND plan_credit_card_id IS NOT NULL AND plan_expense_id IS NOT NULL
               AND plan_id COLLATE "C" ~ '^ipl_[0-9a-f]{32}$'
               AND plan_credit_card_id COLLATE "C" ~ '^card_[0-9a-f]{32}$'
               AND plan_expense_id = expense_id
               AND plan_credit_card_id = expense_credit_card_id
               AND plan_total_minor IS NOT NULL AND plan_total_minor >= 2
               AND plan_total_minor >= plan_installment_count
               AND plan_total_minor = expense_amount_minor
               AND plan_total_currency IS NOT NULL AND plan_total_currency = 'BRL'
               AND plan_installment_count IS NOT NULL AND plan_installment_count BETWEEN 2 AND 120
               AND plan_first_due_on IS NOT NULL AND plan_due_day IS NOT NULL AND plan_due_day BETWEEN 1 AND 31
               AND plan_first_due_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
               AND plan_status IS NOT NULL AND plan_status = 'ACTIVE' AND plan_created_at IS NOT NULL
               AND plan_cancelled_on IS NULL)))
    ),
    CONSTRAINT card_purchase_idem_timestamps_ordered CHECK (completed_at IS NULL OR completed_at >= created_at)
);

CREATE TABLE installment_plan_idempotency_records (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_fingerprint BYTEA NOT NULL,
    state TEXT NOT NULL,
    plan_id TEXT,
    plan_user_id TEXT,
    plan_credit_card_id TEXT,
    plan_expense_id TEXT,
    plan_total_minor BIGINT,
    plan_total_currency TEXT,
    plan_installment_count SMALLINT,
    plan_first_due_on DATE,
    plan_due_day SMALLINT,
    plan_status TEXT,
    plan_created_at TIMESTAMPTZ(6),
    plan_cancelled_on DATE,
    created_at TIMESTAMPTZ(6) NOT NULL,
    completed_at TIMESTAMPTZ(6),
    CONSTRAINT installment_plan_idem_pkey PRIMARY KEY (user_id, operation, idempotency_key),
    CONSTRAINT installment_plan_idem_plan_owner_fkey FOREIGN KEY (plan_user_id)
        REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT installment_plan_idem_operation_valid CHECK (operation = 'CANCEL_INSTALLMENT_PLAN'),
    CONSTRAINT installment_plan_idem_key_valid CHECK (
        octet_length(idempotency_key) BETWEEN 1 AND 128
        AND idempotency_key COLLATE "C" ~ '^[!-~]+$'
    ),
    CONSTRAINT installment_plan_idem_fingerprint_valid CHECK (octet_length(request_fingerprint) = 32),
    CONSTRAINT installment_plan_idem_state_valid CHECK (state IN ('PENDING', 'COMPLETED')),
    CONSTRAINT installment_plan_idem_snapshot_valid CHECK (
        (state = 'PENDING' AND plan_id IS NULL AND plan_user_id IS NULL AND completed_at IS NULL
         AND num_nonnulls(
             plan_user_id, plan_credit_card_id, plan_expense_id, plan_total_minor,
             plan_total_currency, plan_installment_count, plan_first_due_on,
             plan_due_day, plan_status, plan_created_at, plan_cancelled_on
         ) = 0)
        OR
        (state = 'COMPLETED'
         AND plan_id IS NOT NULL
         AND plan_user_id IS NOT NULL AND plan_user_id = user_id
         AND plan_id COLLATE "C" ~ '^ipl_[0-9a-f]{32}$'
         AND plan_credit_card_id IS NOT NULL
         AND plan_credit_card_id COLLATE "C" ~ '^card_[0-9a-f]{32}$'
         AND plan_expense_id IS NOT NULL
         AND plan_total_minor IS NOT NULL AND plan_total_minor > 0
         AND plan_total_currency IS NOT NULL AND plan_total_currency = 'BRL'
         AND plan_installment_count IS NOT NULL AND plan_installment_count BETWEEN 2 AND 120
         AND plan_total_minor >= plan_installment_count
         AND plan_first_due_on IS NOT NULL
         AND plan_first_due_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
         AND plan_due_day IS NOT NULL AND plan_due_day BETWEEN 1 AND 31
         AND plan_status IS NOT NULL AND plan_status = 'CANCELLED'
         AND plan_created_at IS NOT NULL
         AND plan_cancelled_on IS NOT NULL
         AND plan_cancelled_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
         AND completed_at IS NOT NULL)
    ),
    CONSTRAINT installment_plan_idem_timestamps_ordered CHECK (completed_at IS NULL OR completed_at >= created_at)
);

---- create above / drop below ----

-- Writers reserve idempotency before touching aggregates; keep DOWN lock order
-- aligned so a writer cannot hold a later table while DOWN waits on an earlier one.
LOCK TABLE card_purchase_idempotency_records, installment_plan_idempotency_records,
    transactions, installment_plans, installment_plan_audit_events
    IN ACCESS EXCLUSIVE MODE;

DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM installment_plans)
        OR EXISTS (SELECT 1 FROM installment_plan_audit_events)
        OR EXISTS (SELECT 1 FROM card_purchase_idempotency_records)
        OR EXISTS (SELECT 1 FROM installment_plan_idempotency_records)
        OR EXISTS (SELECT 1 FROM transactions WHERE credit_card_id IS NOT NULL OR statement_due_on IS NOT NULL)
    THEN
        RAISE EXCEPTION 'migration 008 downgrade refused: card purchase subsystem contains data';
    END IF;
END
$guard$;

DROP TABLE installment_plan_idempotency_records;
DROP TABLE card_purchase_idempotency_records;
DROP TABLE installment_plan_audit_events;
DROP TABLE installment_plans;

ALTER TABLE transactions
    DROP CONSTRAINT transactions_credit_card_owner_fkey,
    DROP CONSTRAINT transactions_statement_due_on_supported,
    DROP CONSTRAINT transactions_credit_card_id_valid,
    DROP CONSTRAINT transactions_credit_card_payment_method,
    DROP CONSTRAINT transactions_credit_card_linked_together,
    DROP COLUMN credit_card_id,
    DROP COLUMN statement_due_on;
