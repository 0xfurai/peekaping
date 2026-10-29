-- Account emails (PP-152): a password reset link and an email verification link, each a single-use
-- token with a lifetime. The same file runs on SQLite and PostgreSQL.
--
-- account_tokens holds the tokens. A token is 32 random bytes with a prefix (pkr_ for a reset,
-- pkv_ for a verification) that is shown once, in the email, and stored only as a hash. used_at is
-- set when the link is used; a token that was used or has expired does nothing. A user's tokens go
-- with the user.
CREATE TABLE IF NOT EXISTS account_tokens (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    purpose VARCHAR(16) NOT NULL,
    token_hash VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    used_at TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- A user's tokens are looked up by user and purpose, to delete the unused ones of a purpose
CREATE INDEX IF NOT EXISTS idx_account_tokens_user_purpose ON account_tokens(user_id, purpose);

-- When the user's email address was verified. NULL means it was not.
ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMP;

-- Tokens issued before this moment no longer work. A password reset sets it to the time of the
-- reset, which ends every session of the user: the access and refresh tokens carry the time they
-- were issued. NULL means no session was ended.
ALTER TABLE users ADD COLUMN tokens_valid_after TIMESTAMP;

-- An install that upgrades keeps its users as they are: they count as verified, so that nothing
-- changes for them
UPDATE users SET email_verified_at = CURRENT_TIMESTAMP;
