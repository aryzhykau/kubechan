-- Migration 006: Users table

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK(role IN ('admin', 'viewer')),
    created_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
