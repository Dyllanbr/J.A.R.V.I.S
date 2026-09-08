CREATE TABLE monthly_budgets (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    month DATE NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL,
    updated_at TIMESTAMPTZ(6) NOT NULL,
    CONSTRAINT monthly_budgets_pkey PRIMARY KEY (user_id, month),
    CONSTRAINT monthly_budgets_user_id_length CHECK (octet_length(user_id) BETWEEN 1 AND 128),
    CONSTRAINT monthly_budgets_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT monthly_budgets_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT monthly_budgets_month_supported CHECK (
        month BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
        AND EXTRACT(DAY FROM month) = 1
    ),
    CONSTRAINT monthly_budgets_amount_non_negative CHECK (amount_minor >= 0),
    CONSTRAINT monthly_budgets_currency_brl CHECK (currency = 'BRL'),
    CONSTRAINT monthly_budgets_timestamps_ordered CHECK (updated_at >= created_at)
);

---- create above / drop below ----

LOCK TABLE monthly_budgets IN ACCESS EXCLUSIVE MODE;

DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM monthly_budgets) THEN
        RAISE EXCEPTION 'migration 009 downgrade refused: monthly budget subsystem contains data';
    END IF;
END
$guard$;

DROP TABLE monthly_budgets;
