CREATE TABLE recurrence_suggestion_suppressions (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    suggestion_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    evidence_fingerprint BYTEA NOT NULL,
    dismissed_at TIMESTAMPTZ(6) NOT NULL,
    CONSTRAINT recurrence_suggestion_suppressions_pkey PRIMARY KEY (user_id, suggestion_id),
    CONSTRAINT recurrence_suggestion_suppressions_user_id_length CHECK (
        octet_length(user_id) BETWEEN 1 AND 128
    ),
    CONSTRAINT recurrence_suggestion_suppressions_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT recurrence_suggestion_suppressions_user_id_no_controls CHECK (
        user_id !~ '[[:cntrl:]]'
    ),
    CONSTRAINT recurrence_suggestion_suppressions_suggestion_id_valid CHECK (
        suggestion_id COLLATE "C" ~ '^rsg_[0-9a-f]{64}$'
    ),
    CONSTRAINT recurrence_suggestion_suppressions_operation_valid CHECK (
        operation = 'DISMISS_RECURRENCE_SUGGESTION'
    ),
    CONSTRAINT recurrence_suggestion_suppressions_fingerprint_sha256 CHECK (
        octet_length(evidence_fingerprint) = 32
    )
);

---- create above / drop below ----

LOCK TABLE recurrence_suggestion_suppressions IN ACCESS EXCLUSIVE MODE;

DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM recurrence_suggestion_suppressions)
    THEN
        RAISE EXCEPTION 'migration 006 downgrade refused: recurrence suggestion suppressions contain data';
    END IF;
END
$guard$;

DROP TABLE recurrence_suggestion_suppressions;
