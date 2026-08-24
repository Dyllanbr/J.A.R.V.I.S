CREATE TABLE recurrences (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    transaction_type TEXT NOT NULL,
    description TEXT NOT NULL,
    expected_amount_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    frequency TEXT NOT NULL,
    starts_on DATE NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL,
    cancelled_at TIMESTAMPTZ(6),
    CONSTRAINT recurrences_id_length CHECK (octet_length(id) BETWEEN 1 AND 128),
    CONSTRAINT recurrences_id_trimmed CHECK (
        id = btrim(id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT recurrences_id_no_controls CHECK (id !~ '[[:cntrl:]]'),
    CONSTRAINT recurrences_user_id_length CHECK (octet_length(user_id) BETWEEN 1 AND 128),
    CONSTRAINT recurrences_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT recurrences_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT recurrences_id_user_id_unique UNIQUE (id, user_id),
    CONSTRAINT recurrences_type_expense CHECK (transaction_type = 'EXPENSE'),
    CONSTRAINT recurrences_description_valid CHECK (
        description = btrim(description, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
        AND char_length(description) BETWEEN 1 AND 200
    ),
    CONSTRAINT recurrences_amount_positive CHECK (expected_amount_minor > 0),
    CONSTRAINT recurrences_currency_brl CHECK (currency = 'BRL'),
    CONSTRAINT recurrences_frequency_monthly CHECK (frequency = 'MONTHLY'),
    CONSTRAINT recurrences_starts_on_supported CHECK (
        starts_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
    ),
    CONSTRAINT recurrences_status_valid CHECK (status IN ('ACTIVE', 'CANCELLED')),
    CONSTRAINT recurrences_lifecycle_valid CHECK (
        (status = 'ACTIVE' AND cancelled_at IS NULL)
        OR
        (status = 'CANCELLED' AND cancelled_at IS NOT NULL AND cancelled_at >= created_at)
    )
);

CREATE INDEX recurrences_user_id_idx ON recurrences(user_id);

CREATE TABLE recurrence_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    recurrence_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL,
    CONSTRAINT recurrence_audit_events_user_id_length CHECK (octet_length(user_id) BETWEEN 1 AND 128),
    CONSTRAINT recurrence_audit_events_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT recurrence_audit_events_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT recurrence_audit_events_recurrence_id_length CHECK (octet_length(recurrence_id) BETWEEN 1 AND 128),
    CONSTRAINT recurrence_audit_events_recurrence_id_trimmed CHECK (
        recurrence_id = btrim(recurrence_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT recurrence_audit_events_recurrence_id_no_controls CHECK (recurrence_id !~ '[[:cntrl:]]'),
    CONSTRAINT recurrence_audit_events_event_type_valid CHECK (
        event_type IN ('RECURRENCE_CREATED', 'RECURRENCE_CANCELLED')
    ),
    CONSTRAINT recurrence_audit_events_recurrence_owner_fkey FOREIGN KEY (recurrence_id, user_id)
        REFERENCES recurrences(id, user_id) ON DELETE RESTRICT,
    CONSTRAINT recurrence_audit_events_unique_event UNIQUE (recurrence_id, event_type)
);

CREATE TABLE recurrence_idempotency_records (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_fingerprint BYTEA NOT NULL,
    state TEXT NOT NULL,
    recurrence_id TEXT,
    result_transaction_type TEXT,
    result_description TEXT,
    result_expected_amount_minor BIGINT,
    result_currency TEXT,
    result_frequency TEXT,
    result_starts_on DATE,
    result_status TEXT,
    result_created_at TIMESTAMPTZ(6),
    result_cancelled_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL,
    completed_at TIMESTAMPTZ(6),
    CONSTRAINT recurrence_idempotency_records_pkey PRIMARY KEY (user_id, operation, idempotency_key),
    CONSTRAINT recurrence_idempotency_records_user_id_length CHECK (octet_length(user_id) BETWEEN 1 AND 128),
    CONSTRAINT recurrence_idempotency_records_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT recurrence_idempotency_records_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT recurrence_idempotency_records_operation_valid CHECK (
        operation IN ('CREATE_RECURRENCE', 'CANCEL_RECURRENCE')
    ),
    CONSTRAINT recurrence_idempotency_records_key_length CHECK (octet_length(idempotency_key) BETWEEN 1 AND 128),
    CONSTRAINT recurrence_idempotency_records_key_visible_ascii CHECK (
        (idempotency_key COLLATE "C") ~ '^[!-~]+$'
    ),
    CONSTRAINT recurrence_idempotency_records_fingerprint_sha256 CHECK (
        octet_length(request_fingerprint) = 32
    ),
    CONSTRAINT recurrence_idempotency_records_state_valid CHECK (state IN ('PENDING', 'COMPLETED')),
    CONSTRAINT recurrence_idempotency_records_result_valid CHECK (
        (
            state = 'PENDING'
            AND recurrence_id IS NULL
            AND completed_at IS NULL
            AND num_nonnulls(
                result_transaction_type,
                result_description,
                result_expected_amount_minor,
                result_currency,
                result_frequency,
                result_starts_on,
                result_status,
                result_created_at,
                result_cancelled_at
            ) = 0
        )
        OR
        (
            state = 'COMPLETED'
            AND recurrence_id IS NOT NULL
            AND completed_at IS NOT NULL
            AND num_nonnulls(
                result_transaction_type,
                result_description,
                result_expected_amount_minor,
                result_currency,
                result_frequency,
                result_starts_on,
                result_status,
                result_created_at
            ) = 8
            AND result_transaction_type = 'EXPENSE'
            AND result_description = btrim(result_description, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
            AND char_length(result_description) BETWEEN 1 AND 200
            AND result_expected_amount_minor > 0
            AND result_currency = 'BRL'
            AND result_frequency = 'MONTHLY'
            AND result_starts_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
            AND (
                (
                    operation = 'CREATE_RECURRENCE'
                    AND result_status = 'ACTIVE'
                    AND result_cancelled_at IS NULL
                )
                OR
                (
                    operation = 'CANCEL_RECURRENCE'
                    AND result_status = 'CANCELLED'
                    AND result_cancelled_at IS NOT NULL
                    AND result_cancelled_at >= result_created_at
                )
            )
        )
    ),
    CONSTRAINT recurrence_idempotency_records_timestamps_ordered CHECK (
        completed_at IS NULL OR completed_at >= created_at
    ),
    CONSTRAINT recurrence_idempotency_records_recurrence_owner_fkey FOREIGN KEY (recurrence_id, user_id)
        REFERENCES recurrences(id, user_id) ON DELETE RESTRICT
);

---- create above / drop below ----

LOCK TABLE recurrence_idempotency_records, recurrences, recurrence_audit_events
    IN ACCESS EXCLUSIVE MODE;

DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM recurrence_idempotency_records)
        OR EXISTS (SELECT 1 FROM recurrence_audit_events)
        OR EXISTS (SELECT 1 FROM recurrences)
    THEN
        RAISE EXCEPTION 'migration 005 downgrade refused: recurrence subsystem contains data';
    END IF;
END
$guard$;

DROP TABLE recurrence_idempotency_records;
DROP TABLE recurrence_audit_events;
DROP TABLE recurrences;
