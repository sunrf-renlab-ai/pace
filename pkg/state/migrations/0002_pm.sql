-- v0.3 PM layer: per-project goals, generated plan documents.
-- Focus is stored in user_prefs (key="focus.project_path", "focus.reason", "focus.until")
-- so it can be read/written by the existing setpref machinery.

CREATE TABLE goals (
  project_path     TEXT PRIMARY KEY,
  description      TEXT NOT NULL,
  deadline         TIMESTAMP,
  milestones_json  TEXT,                -- JSON array of {name, done}
  set_at           TIMESTAMP NOT NULL,
  source           TEXT NOT NULL        -- 'cli' | 'chat'
);

CREATE TABLE plans (
  plan_id      TEXT PRIMARY KEY,
  generated_at TIMESTAMP NOT NULL,
  scope        TEXT NOT NULL,           -- 'today' | 'week' | 'manual'
  content_md   TEXT NOT NULL,
  rationale    TEXT,
  source       TEXT NOT NULL            -- 'rule:R9' | 'cli' | 'chat'
);
CREATE INDEX idx_plans_generated ON plans(generated_at DESC);

INSERT INTO schema_version (version) VALUES (2);
