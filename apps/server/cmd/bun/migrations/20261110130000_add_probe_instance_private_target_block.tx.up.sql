-- Whether a probe instance blocks private targets when the server does (PP-156). A probe from
-- before BLOCK_PRIVATE_TARGETS ignores the server's setting, and a server that blocks private
-- targets deals no monitors to an instance whose hello didn't say it enforces the block. The same
-- file runs on SQLite and PostgreSQL.
--
-- Every existing instance said hello before the field existed, so none does until it says hello
-- again.
ALTER TABLE probe_instances ADD COLUMN enforces_private_target_block BOOLEAN NOT NULL DEFAULT false;
