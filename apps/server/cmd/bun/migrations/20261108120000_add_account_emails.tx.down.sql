ALTER TABLE users DROP COLUMN tokens_valid_after;
ALTER TABLE users DROP COLUMN email_verified_at;
DROP TABLE IF EXISTS account_tokens;
