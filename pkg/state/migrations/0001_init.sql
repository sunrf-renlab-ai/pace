CREATE TABLE events (
  event_id     TEXT PRIMARY KEY,
  timestamp    TIMESTAMP NOT NULL,
  hook_type    TEXT NOT NULL,
  session_id   TEXT NOT NULL,
  project_path TEXT NOT NULL,
  payload_json TEXT NOT NULL
);
CREATE INDEX idx_events_project_ts ON events(project_path, timestamp DESC);
CREATE INDEX idx_events_session ON events(session_id);

CREATE TABLE projects (
  project_path     TEXT PRIMARY KEY,
  display_name     TEXT,
  first_seen       TIMESTAMP NOT NULL,
  last_active      TIMESTAMP NOT NULL,
  inferred_focus   TEXT,
  paused_until     TIMESTAMP
);

CREATE TABLE actions (
  action_id        TEXT PRIMARY KEY,
  timestamp        TIMESTAMP NOT NULL,
  action_type      TEXT NOT NULL,
  project_path     TEXT,
  trigger_event_id TEXT,
  rationale        TEXT NOT NULL,
  parameters_json  TEXT NOT NULL,
  status           TEXT NOT NULL,
  result_summary   TEXT,
  undo_payload     TEXT
);
CREATE INDEX idx_actions_ts ON actions(timestamp DESC);

CREATE TABLE user_prefs (
  key    TEXT PRIMARY KEY,
  value  TEXT NOT NULL,
  set_at TIMESTAMP NOT NULL,
  source TEXT NOT NULL
);

CREATE TABLE chat_log (
  message_id TEXT PRIMARY KEY,
  timestamp  TIMESTAMP NOT NULL,
  role       TEXT NOT NULL,
  content    TEXT NOT NULL
);

INSERT INTO schema_version (version) VALUES (1);
