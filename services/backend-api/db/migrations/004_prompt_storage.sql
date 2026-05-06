-- Migration 004: store the LLM prompt sent for each analysis result

ALTER TABLE analysis_results ADD COLUMN prompt TEXT;
