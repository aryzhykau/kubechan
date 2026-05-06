-- Per-user LLM provider preference and credentials.
-- credentials is a JSON blob; shape is provider-specific:
--   bedrock:  {"accessKeyId":"...","secretAccessKey":"...","region":"us-east-1","modelId":"qwen.qwen3-32b-v1:0"}
--   copilot:  {"token":"github_pat_...","modelId":"gpt-4o"}
CREATE TABLE user_llm_settings (
    user_id     TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL DEFAULT 'bedrock' CHECK(provider IN ('bedrock', 'copilot')),
    credentials TEXT NOT NULL DEFAULT '{}',
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
