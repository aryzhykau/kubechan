-- Detector threshold settings configurable by admins.
INSERT OR IGNORE INTO settings(key, value) VALUES
    ('detector.debounce_window_secs',       '30'),
    ('detector.pending_threshold_secs',     '300'),
    ('detector.unavailable_threshold_secs', '300');
