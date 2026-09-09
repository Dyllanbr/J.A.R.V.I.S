CREATE TABLE auth_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    token_hash BYTEA NOT NULL UNIQUE,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT auth_sessions_id_length CHECK (octet_length(id) BETWEEN 1 AND 128),
    CONSTRAINT auth_sessions_id_trimmed CHECK (
        id = btrim(id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT auth_sessions_id_no_controls CHECK (id !~ '[[:cntrl:]]'),
    CONSTRAINT auth_sessions_user_id_length CHECK (octet_length(user_id) BETWEEN 1 AND 128),
    CONSTRAINT auth_sessions_user_id_trimmed CHECK (
        user_id = btrim(user_id, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
    ),
    CONSTRAINT auth_sessions_user_id_no_controls CHECK (user_id !~ '[[:cntrl:]]'),
    CONSTRAINT auth_sessions_token_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT auth_sessions_expiry_ordered CHECK (expires_at > issued_at),
    CONSTRAINT auth_sessions_created_at_present CHECK (created_at IS NOT NULL)
);

CREATE INDEX auth_sessions_user_id_idx ON auth_sessions(user_id);
CREATE INDEX auth_sessions_active_lookup_idx ON auth_sessions(token_hash, expires_at)
    WHERE revoked_at IS NULL;

---- create above / drop below ----

LOCK TABLE auth_sessions IN ACCESS EXCLUSIVE MODE;

DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM auth_sessions) THEN
        RAISE EXCEPTION 'migration 011 downgrade refused: authentication sessions contain data';
    END IF;
END
$guard$;

DROP TABLE auth_sessions;
