-- The monitor types each probe instance can run (#264). Until now a probe held one list, which the
-- last hello overwrote, so instances of one probe on different builds (the full image and the
-- slim one) flipped it. The probe's list is now the union over its online instances, which the
-- server works out from these. The same file runs on SQLite and PostgreSQL.
--
-- An empty list means every type, as it does for a probe. Each existing instance takes the list of
-- its probe, which is what every instance ran as until now: the probe's list was the last hello's.
ALTER TABLE probe_instances ADD COLUMN supported_types JSONB NOT NULL DEFAULT '[]';

UPDATE probe_instances
SET supported_types = (SELECT probes.supported_types FROM probes WHERE probes.id = probe_instances.probe_id);
