CREATE TABLE financial_goals (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    id TEXT NOT NULL,
    title TEXT NOT NULL,
    target_amount_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    CONSTRAINT financial_goals_pkey PRIMARY KEY (user_id, id),
    CONSTRAINT financial_goals_user_id_length CHECK (octet_length(user_id) BETWEEN 1 AND 128),
    CONSTRAINT financial_goals_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT financial_goals_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT financial_goals_id_length CHECK (octet_length(id) BETWEEN 1 AND 128),
    CONSTRAINT financial_goals_id_trimmed CHECK (
        id = btrim(id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT financial_goals_id_no_controls CHECK (id !~ '[[:cntrl:]]'),
    CONSTRAINT financial_goals_title_length CHECK (octet_length(title) BETWEEN 1 AND 800),
    CONSTRAINT financial_goals_title_trimmed CHECK (
        title = btrim(title, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT financial_goals_title_no_controls CHECK (title !~ '[[:cntrl:]]'),
    CONSTRAINT financial_goals_target_positive CHECK (target_amount_minor > 0),
    CONSTRAINT financial_goals_currency_brl CHECK (currency = 'BRL')
);

CREATE TABLE protected_values (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    id TEXT NOT NULL,
    label TEXT NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    CONSTRAINT protected_values_pkey PRIMARY KEY (user_id, id),
    CONSTRAINT protected_values_user_id_length CHECK (octet_length(user_id) BETWEEN 1 AND 128),
    CONSTRAINT protected_values_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT protected_values_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT protected_values_id_length CHECK (octet_length(id) BETWEEN 1 AND 128),
    CONSTRAINT protected_values_id_trimmed CHECK (
        id = btrim(id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT protected_values_id_no_controls CHECK (id !~ '[[:cntrl:]]'),
    CONSTRAINT protected_values_label_length CHECK (octet_length(label) BETWEEN 1 AND 800),
    CONSTRAINT protected_values_label_trimmed CHECK (
        label = btrim(label, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT protected_values_label_no_controls CHECK (label !~ '[[:cntrl:]]'),
    CONSTRAINT protected_values_amount_non_negative CHECK (amount_minor >= 0),
    CONSTRAINT protected_values_currency_brl CHECK (currency = 'BRL')
);

---- create above / drop below ----

LOCK TABLE protected_values, financial_goals IN ACCESS EXCLUSIVE MODE;

DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM protected_values) OR EXISTS (SELECT 1 FROM financial_goals) THEN
        RAISE EXCEPTION 'migration 010 downgrade refused: financial goals subsystem contains data';
    END IF;
END
$guard$;

DROP TABLE protected_values;
DROP TABLE financial_goals;
