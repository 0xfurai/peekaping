DROP TABLE IF EXISTS monitor_slow_response_states;
ALTER TABLE monitors DROP COLUMN slow_response_consecutive;
ALTER TABLE monitors DROP COLUMN slow_response_min_ms;
ALTER TABLE monitors DROP COLUMN slow_response_multiplier;
ALTER TABLE monitors DROP COLUMN slow_response_threshold_ms;
ALTER TABLE monitors DROP COLUMN slow_response_mode;
