CREATE TABLE users (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT users_id_length CHECK (octet_length(id) BETWEEN 1 AND 128),
    -- Explicit Unicode White_Space set used by Go 1.26.6 strings.TrimSpace.
    CONSTRAINT users_id_trimmed CHECK (
        id = btrim(id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT users_id_no_controls CHECK (id !~ '[[:cntrl:]]'),
    CONSTRAINT users_timestamps_ordered CHECK (updated_at >= created_at)
);

CREATE TABLE transactions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    type TEXT NOT NULL,
    description TEXT NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    payment_method TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    financial_timezone TEXT NOT NULL,
    origin TEXT NOT NULL,
    status TEXT NOT NULL,
    version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT transactions_id_length CHECK (octet_length(id) BETWEEN 1 AND 128),
    CONSTRAINT transactions_id_trimmed CHECK (
        id = btrim(id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT transactions_id_no_controls CHECK (id !~ '[[:cntrl:]]'),
    CONSTRAINT transactions_user_id_length CHECK (octet_length(user_id) BETWEEN 1 AND 128),
    CONSTRAINT transactions_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT transactions_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT transactions_id_user_id_unique UNIQUE (id, user_id),
    CONSTRAINT transactions_type_expense CHECK (type = 'EXPENSE'),
    CONSTRAINT transactions_description_valid CHECK (
        description = btrim(description, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
        AND char_length(description) BETWEEN 1 AND 200
    ),
    CONSTRAINT transactions_amount_positive CHECK (amount_minor > 0),
    CONSTRAINT transactions_currency_brl CHECK (currency = 'BRL'),
    CONSTRAINT transactions_payment_method_valid CHECK (payment_method IN ('PIX', 'DEBIT', 'CREDIT', 'CASH')),
    CONSTRAINT transactions_timezone_present CHECK (
        char_length(financial_timezone) > 0
        AND financial_timezone = btrim(financial_timezone, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT transactions_origin_valid CHECK (origin IN ('IOS', 'WHATSAPP')),
    CONSTRAINT transactions_status_recorded CHECK (status = 'RECORDED'),
    CONSTRAINT transactions_version_positive CHECK (version >= 1),
    CONSTRAINT transactions_timestamps_ordered CHECK (updated_at >= created_at)
);

CREATE INDEX transactions_user_id_idx ON transactions(user_id);

CREATE TABLE audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    aggregate_version BIGINT NOT NULL,
    event_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT audit_events_user_id_length CHECK (octet_length(user_id) BETWEEN 1 AND 128),
    CONSTRAINT audit_events_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT audit_events_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT audit_events_aggregate_type_expense CHECK (aggregate_type = 'EXPENSE'),
    CONSTRAINT audit_events_aggregate_id_length CHECK (octet_length(aggregate_id) BETWEEN 1 AND 128),
    CONSTRAINT audit_events_aggregate_id_trimmed CHECK (
        aggregate_id = btrim(aggregate_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT audit_events_aggregate_id_no_controls CHECK (aggregate_id !~ '[[:cntrl:]]'),
    CONSTRAINT audit_events_aggregate_version_positive CHECK (aggregate_version >= 1),
    CONSTRAINT audit_events_event_type_recorded CHECK (event_type = 'EXPENSE_RECORDED'),
    CONSTRAINT audit_events_transaction_owner_fkey FOREIGN KEY (aggregate_id, user_id)
        REFERENCES transactions(id, user_id) ON DELETE RESTRICT,
    CONSTRAINT audit_events_unique_version UNIQUE (
        aggregate_type,
        aggregate_id,
        aggregate_version,
        event_type
    )
);

CREATE INDEX audit_events_user_id_idx ON audit_events(user_id);
CREATE INDEX audit_events_aggregate_id_idx ON audit_events(aggregate_id);

---- create above / drop below ----

DROP TABLE audit_events;
DROP TABLE transactions;
DROP TABLE users;
