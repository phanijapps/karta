-- Chitta schema (design D5). Idempotent: guarded by the schema-version
-- node check in db.go; every statement must tolerate re-execution via
-- IF NOT EXISTS.

CREATE NODE TABLE IF NOT EXISTS ChittaMeta(key STRING PRIMARY KEY, value STRING);
CREATE NODE TABLE IF NOT EXISTS Memory(
  id STRING PRIMARY KEY,
  kind STRING,
  content STRING,
  normalized_content STRING,
  tags STRING[],
  confidence DOUBLE,
  subject STRING,
  status STRING,
  supersedes STRING,
  superseded_by STRING,
  entities STRING[],
  mention_count INT64,
  last_confirmed_at TIMESTAMP,
  source STRING,
  session_id STRING,
  execution_id STRING,
  agent_id STRING,
  message_seq INT64,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  embedding FLOAT[%EMB_DIM%]
);
CREATE NODE TABLE IF NOT EXISTS Contribution(
  id STRING PRIMARY KEY,
  record_id STRING,
  source STRING,
  session_id STRING,
  execution_id STRING,
  message_seq INT64,
  content_hash STRING,
  created_at TIMESTAMP
);
CREATE NODE TABLE IF NOT EXISTS Entity(
  id STRING PRIMARY KEY,
  type STRING,
  name STRING,
  normalized_name STRING,
  aliases STRING[],
  confidence DOUBLE,
  mention_count INT64,
  last_mentioned_at TIMESTAMP,
  embedding DOUBLE[]      -- future entity-similarity; unwritten in v1
);
CREATE REL TABLE IF NOT EXISTS Relates(
  FROM Entity TO Entity,
  id STRING,
  type STRING,
  confidence DOUBLE,
  source STRING,
  session_id STRING,
  execution_id STRING,
  agent_id STRING,
  created_at TIMESTAMP
);
-- Declared for future graph-side traversals (distiller); UNWRITTEN in
-- v1: record.entities membership is the normative link today.
CREATE REL TABLE IF NOT EXISTS Mentions(
  FROM Memory TO Entity,
  created_at TIMESTAMP
);
