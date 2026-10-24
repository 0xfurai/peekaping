-- The probe a monitor runs on. Until a monitor can run on several probes (#36) it has exactly one
-- row here, which the monitor service keeps. The same file runs on SQLite and PostgreSQL.

-- Deleting a monitor deletes its row. The probe has no ON DELETE: the database itself refuses to
-- delete a probe that still has monitors.
CREATE TABLE IF NOT EXISTS monitor_probes (
    monitor_id UUID NOT NULL,
    probe_id VARCHAR(255) NOT NULL,
    PRIMARY KEY (monitor_id, probe_id),
    FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE,
    FOREIGN KEY (probe_id) REFERENCES probes(id)
);

-- The probe protocol lists the monitors of one probe
CREATE INDEX IF NOT EXISTS idx_monitor_probes_probe_id ON monitor_probes(probe_id);

-- Every monitor that exists runs on the local probe, as it has until now. A monitor that already
-- has a row keeps it, so a re-run leaves a monitor that has been moved where it is.
INSERT INTO monitor_probes (monitor_id, probe_id)
SELECT id, 'local' FROM monitors
WHERE NOT EXISTS (SELECT 1 FROM monitor_probes WHERE monitor_probes.monitor_id = monitors.id);
