-- v0.4 mentor layer: opinions are structured judgments produced by brain in
-- "mentor mode" (two-pass adversarial review). Each opinion has a clear
-- WHAT (observation), a WHY (concern), a HOW (recommendation), and a
-- confidence label. Status tracks whether the user has acknowledged or
-- dismissed it (so we don't keep nagging).

CREATE TABLE mentor_opinions (
  opinion_id      TEXT PRIMARY KEY,
  created_at      TIMESTAMP NOT NULL,
  trigger         TEXT NOT NULL,         -- 'R11.commit_review' | 'R15.pulse' | 'cli:ask' | 'cli:review' | 'cli:consult'
  project_path    TEXT,                  -- nullable for cross-project opinions
  topic           TEXT NOT NULL,         -- short headline
  observation     TEXT NOT NULL,         -- what was seen, with evidence (file:line, commit sha, etc.)
  concern         TEXT,                  -- why it matters
  recommendation  TEXT,                  -- what to do about it
  confidence      TEXT NOT NULL,         -- 'high' | 'medium' | 'low'
  evidence_json   TEXT,                  -- raw refs (commit shas, file paths)
  status          TEXT NOT NULL DEFAULT 'open'  -- 'open' | 'acknowledged' | 'dismissed'
);
CREATE INDEX idx_mentor_opinions_created ON mentor_opinions(created_at DESC);
CREATE INDEX idx_mentor_opinions_status ON mentor_opinions(status);

INSERT INTO schema_version (version) VALUES (3);
