CREATE TABLE credit_cards (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    last_four TEXT,
    brand TEXT,
    closing_day SMALLINT NOT NULL,
    due_day SMALLINT NOT NULL,
    credit_limit_minor BIGINT,
    credit_limit_currency TEXT,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL,
    archived_at TIMESTAMPTZ(6),
    CONSTRAINT credit_cards_id_valid CHECK (
        id COLLATE "C" ~ '^card_[0-9a-f]{32}$'
    ),
    CONSTRAINT credit_cards_user_id_length CHECK (
        octet_length(user_id) BETWEEN 1 AND 128
    ),
    CONSTRAINT credit_cards_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT credit_cards_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT credit_cards_id_user_id_unique UNIQUE (id, user_id),
    CONSTRAINT credit_cards_name_valid CHECK (
        name = btrim(name, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
        AND char_length(name) BETWEEN 1 AND 200
        AND name !~ '[[:cntrl:]]'
    ),
    CONSTRAINT credit_cards_last_four_valid CHECK (
        last_four IS NULL OR last_four COLLATE "C" ~ '^[0-9]{4}$'
    ),
    CONSTRAINT credit_cards_brand_valid CHECK (
        brand IS NULL OR brand IN ('VISA', 'MASTERCARD', 'ELO', 'AMERICAN_EXPRESS', 'HIPERCARD', 'OTHER')
    ),
    CONSTRAINT credit_cards_closing_day_valid CHECK (closing_day BETWEEN 1 AND 31),
    CONSTRAINT credit_cards_due_day_valid CHECK (due_day BETWEEN 1 AND 31),
    CONSTRAINT credit_cards_limit_valid CHECK (
        (credit_limit_minor IS NULL AND credit_limit_currency IS NULL)
        OR
        (
            credit_limit_minor IS NOT NULL
            AND credit_limit_currency IS NOT NULL
            AND credit_limit_minor > 0
            AND credit_limit_currency = 'BRL'
        )
    ),
    CONSTRAINT credit_cards_status_valid CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    CONSTRAINT credit_cards_lifecycle_valid CHECK (
        status NOT IN ('ACTIVE', 'ARCHIVED')
        OR
        (status = 'ACTIVE' AND archived_at IS NULL)
        OR
        (status = 'ARCHIVED' AND archived_at IS NOT NULL AND archived_at >= created_at)
    )
);

CREATE INDEX credit_cards_user_id_idx ON credit_cards(user_id);

CREATE TABLE credit_card_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    credit_card_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL,
    CONSTRAINT credit_card_audit_events_user_id_length CHECK (
        octet_length(user_id) BETWEEN 1 AND 128
    ),
    CONSTRAINT credit_card_audit_events_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT credit_card_audit_events_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT credit_card_audit_events_card_id_valid CHECK (
        credit_card_id COLLATE "C" ~ '^card_[0-9a-f]{32}$'
    ),
    CONSTRAINT credit_card_audit_events_type_valid CHECK (
        event_type IN ('CREDIT_CARD_CREATED', 'CREDIT_CARD_ARCHIVED')
    ),
    CONSTRAINT credit_card_audit_events_owner_fkey FOREIGN KEY (credit_card_id, user_id)
        REFERENCES credit_cards(id, user_id) ON DELETE RESTRICT,
    CONSTRAINT credit_card_audit_events_unique_event UNIQUE (credit_card_id, event_type)
);

CREATE TABLE credit_card_idempotency_records (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_fingerprint BYTEA NOT NULL,
    state TEXT NOT NULL,
    credit_card_id TEXT,
    result_name TEXT,
    result_last_four TEXT,
    result_brand TEXT,
    result_closing_day SMALLINT,
    result_due_day SMALLINT,
    result_credit_limit_minor BIGINT,
    result_credit_limit_currency TEXT,
    result_status TEXT,
    result_created_at TIMESTAMPTZ(6),
    result_archived_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL,
    completed_at TIMESTAMPTZ(6),
    CONSTRAINT credit_card_idempotency_records_pkey PRIMARY KEY (
        user_id, operation, idempotency_key
    ),
    CONSTRAINT credit_card_idem_user_id_length CHECK (
        octet_length(user_id) BETWEEN 1 AND 128
    ),
    CONSTRAINT credit_card_idem_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT credit_card_idem_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT credit_card_idem_operation_valid CHECK (
        operation IN ('CREATE_CREDIT_CARD', 'ARCHIVE_CREDIT_CARD')
    ),
    CONSTRAINT credit_card_idem_key_length CHECK (
        octet_length(idempotency_key) BETWEEN 1 AND 128
    ),
    CONSTRAINT credit_card_idem_key_visible_ascii CHECK (
        idempotency_key COLLATE "C" ~ '^[!-~]+$'
    ),
    CONSTRAINT credit_card_idem_fingerprint_sha256 CHECK (
        octet_length(request_fingerprint) = 32
    ),
    CONSTRAINT credit_card_idem_state_valid CHECK (state IN ('PENDING', 'COMPLETED')),
    CONSTRAINT credit_card_idem_result_valid CHECK (
        state NOT IN ('PENDING', 'COMPLETED')
        OR
        (
            state = 'PENDING'
            AND credit_card_id IS NULL
            AND completed_at IS NULL
            AND num_nonnulls(
                result_name,
                result_last_four,
                result_brand,
                result_closing_day,
                result_due_day,
                result_credit_limit_minor,
                result_credit_limit_currency,
                result_status,
                result_created_at,
                result_archived_at
            ) = 0
        )
        OR
        (
            state = 'COMPLETED'
            AND credit_card_id IS NOT NULL
            AND completed_at IS NOT NULL
            AND result_name IS NOT NULL
            AND result_closing_day IS NOT NULL
            AND result_due_day IS NOT NULL
            AND result_status IS NOT NULL
            AND result_created_at IS NOT NULL
            AND result_name = btrim(result_name, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
            AND char_length(result_name) BETWEEN 1 AND 200
            AND result_name !~ '[[:cntrl:]]'
            AND (result_last_four IS NULL OR result_last_four COLLATE "C" ~ '^[0-9]{4}$')
            AND (result_brand IS NULL OR result_brand IN ('VISA', 'MASTERCARD', 'ELO', 'AMERICAN_EXPRESS', 'HIPERCARD', 'OTHER'))
            AND result_closing_day BETWEEN 1 AND 31
            AND result_due_day BETWEEN 1 AND 31
            AND (
                (result_credit_limit_minor IS NULL AND result_credit_limit_currency IS NULL)
                OR
                (
                    result_credit_limit_minor IS NOT NULL
                    AND result_credit_limit_currency IS NOT NULL
                    AND result_credit_limit_minor > 0
                    AND result_credit_limit_currency = 'BRL'
                )
            )
            AND (
                (
                    operation = 'CREATE_CREDIT_CARD'
                    AND result_status = 'ACTIVE'
                    AND result_archived_at IS NULL
                )
                OR
                (
                    operation = 'ARCHIVE_CREDIT_CARD'
                    AND result_status = 'ARCHIVED'
                    AND result_archived_at IS NOT NULL
                    AND result_archived_at >= result_created_at
                )
            )
        )
    ),
    CONSTRAINT credit_card_idem_timestamps_ordered CHECK (
        completed_at IS NULL OR completed_at >= created_at
    ),
    CONSTRAINT credit_card_idem_owner_fkey FOREIGN KEY (credit_card_id, user_id)
        REFERENCES credit_cards(id, user_id) ON DELETE RESTRICT
);

---- create above / drop below ----

LOCK TABLE credit_card_idempotency_records, credit_cards, credit_card_audit_events
    IN ACCESS EXCLUSIVE MODE;

DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM credit_card_idempotency_records)
        OR EXISTS (SELECT 1 FROM credit_card_audit_events)
        OR EXISTS (SELECT 1 FROM credit_cards)
    THEN
        RAISE EXCEPTION 'migration 007 downgrade refused: credit card subsystem contains data';
    END IF;
END
$guard$;

DROP TABLE credit_card_idempotency_records;
DROP TABLE credit_card_audit_events;
DROP TABLE credit_cards;
