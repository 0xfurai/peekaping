-- Verifying custom domains by DNS (PP-165). The same file runs on SQLite and PostgreSQL.
--
-- verification_token is the secret a domain's owner puts in a TXT record to prove the domain is
-- theirs. It starts empty: the server gives a row its token the first time it shows or checks it,
-- so rows that exist now, and rows the MongoDB import writes, need nothing here.
--
-- verified_at is when the domain was found pointing at this install, NULL while it is Pending. No
-- existing row is verified, so an install that turns VERIFY_CUSTOM_DOMAINS on starts every domain
-- as Pending, and the next check verifies the ones that already point here. An install that leaves
-- it off serves every domain as before.
--
-- pending_since is when the check first found a Pending domain unverified with verification on,
-- and failing_since when it first found a Verified domain's records missing. Both are set by the
-- check, so a domain's days to verify count only while verification is on. last_checked_at is the
-- last check of either kind.
ALTER TABLE domain_status_page ADD COLUMN verification_token TEXT NOT NULL DEFAULT '';
ALTER TABLE domain_status_page ADD COLUMN verified_at TIMESTAMP;
ALTER TABLE domain_status_page ADD COLUMN pending_since TIMESTAMP;
ALTER TABLE domain_status_page ADD COLUMN failing_since TIMESTAMP;
ALTER TABLE domain_status_page ADD COLUMN last_checked_at TIMESTAMP;
