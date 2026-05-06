ALTER TABLE analysis_results ADD COLUMN needs_more_info INTEGER NOT NULL DEFAULT 0;
ALTER TABLE analysis_results ADD COLUMN suggested_resources TEXT;
