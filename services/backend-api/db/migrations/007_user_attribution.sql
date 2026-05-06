-- Migration 007: User attribution for analysis and manual incident ownership

-- Track which user triggered each analysis request
ALTER TABLE analysis_requests ADD COLUMN triggered_by TEXT REFERENCES users(id);

-- Ownership of manual incidents (incidents live as K8s CRDs;
-- ownership metadata is stored in SQLite alongside them)
CREATE TABLE manual_incident_owners (
    incident_id TEXT PRIMARY KEY,
    namespace   TEXT NOT NULL,
    owner_id    TEXT NOT NULL REFERENCES users(id),
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_mio_owner ON manual_incident_owners(owner_id);
