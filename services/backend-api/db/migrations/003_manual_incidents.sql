-- Migration 003: source + user_message columns for manual incidents

ALTER TABLE evidence          ADD COLUMN source       TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE evidence          ADD COLUMN user_message TEXT;
ALTER TABLE analysis_results  ADD COLUMN source       TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE analysis_requests ADD COLUMN source       TEXT NOT NULL DEFAULT 'auto';
