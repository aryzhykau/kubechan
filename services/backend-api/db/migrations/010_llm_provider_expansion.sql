-- Expand per-user LLM settings to providers routed through LiteLLM.
-- SQLite cannot alter a CHECK constraint, so rebuild the small settings table.
CREATE TABLE user_llm_settings_next (
    user_id     TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL DEFAULT 'bedrock'
                CHECK(provider IN ('bedrock', 'copilot', 'openai', 'anthropic')),
    credentials TEXT NOT NULL DEFAULT '{}',
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO user_llm_settings_next (user_id, provider, credentials, updated_at)
SELECT user_id, provider, credentials, updated_at FROM user_llm_settings;

DROP TABLE user_llm_settings;
ALTER TABLE user_llm_settings_next RENAME TO user_llm_settings;
