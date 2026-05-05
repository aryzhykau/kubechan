-- Migration 002: User rating on analysis results

ALTER TABLE analysis_results ADD COLUMN user_rating TEXT
    CHECK(user_rating IN ('up', 'down'));
