-- Keep old handler-duration samples distinct from database-only measurements.
ALTER TABLE op_query_latency ADD COLUMN IF NOT EXISTS timing_kind text NOT NULL DEFAULT 'handler';
ALTER TABLE op_query_latency ALTER COLUMN duration_ms TYPE double precision;
