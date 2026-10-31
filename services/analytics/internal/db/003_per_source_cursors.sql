-- A multi-silo agent has an independent WAL LSN for every source database.
ALTER TABLE agent_cursors
    ADD COLUMN IF NOT EXISTS source VARCHAR(128) NOT NULL DEFAULT 'default';

ALTER TABLE agent_cursors DROP CONSTRAINT IF EXISTS agent_cursors_pkey;
ALTER TABLE agent_cursors ADD PRIMARY KEY (agent_id, source);