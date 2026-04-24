// Package store provides SQLite-backed persistence for the rewards and
// bandwidth services. A single database file holds all tables; each service
// opens the same file and operates on its own tables.
//
// The database is created automatically on first use. All schema migrations
// are applied via CREATE TABLE IF NOT EXISTS, so the file can be opened by
// any version of the binary without manual migration steps.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

// Open opens (or creates) the SQLite database at the given path and applies
// all schema migrations. The returned *sql.DB is safe for concurrent use.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("store: create directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// Limit to one open connection so that PRAGMAs applied below are
	// effective for all queries. SQLite's WAL mode allows concurrent readers
	// but only one writer; a single connection pool entry serialises writes
	// without SQLITE_BUSY errors.
	db.SetMaxOpenConns(1)
	// modernc.org/sqlite ignores DSN pragmas; apply them via Exec instead.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", pragma, err)
		}
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return db, nil
}

// migrate applies all schema DDL. Safe to call on an existing database.
func migrate(db *sql.DB) error {
	stmts := []string{
		// rewards: wallet registrations
		`CREATE TABLE IF NOT EXISTS wallets (
			username        TEXT PRIMARY KEY,
			wallet_address  TEXT NOT NULL,
			uphold_card_id  TEXT NOT NULL DEFAULT '',
			registered_at   TEXT NOT NULL
		)`,
		// rewards: pending/paid reward records
		`CREATE TABLE IF NOT EXISTS rewards (
			id                   TEXT PRIMARY KEY,
			event_type           TEXT NOT NULL,
			project_id           INTEGER NOT NULL DEFAULT 0,
			project_path         TEXT NOT NULL DEFAULT '',
			contributor_username TEXT NOT NULL DEFAULT '',
			contributor_email    TEXT NOT NULL DEFAULT '',
			object_id            INTEGER NOT NULL DEFAULT 0,
			amount_bat           REAL NOT NULL,
			status               TEXT NOT NULL DEFAULT 'pending',
			queued_at            TEXT NOT NULL,
			paid_at              TEXT,
			tx_hash              TEXT NOT NULL DEFAULT ''
		)`,
		// bandwidth: artifact retention records
		`CREATE TABLE IF NOT EXISTS artifacts (
			path        TEXT PRIMARY KEY,
			size_bytes  INTEGER NOT NULL,
			created_at  TEXT NOT NULL,
			project_id  INTEGER NOT NULL DEFAULT 0,
			job_id      INTEGER NOT NULL DEFAULT 0
		)`,
		// generic key-value settings store (used for persisting reward rates, etc.)
		`CREATE TABLE IF NOT EXISTS settings (
			key    TEXT PRIMARY KEY,
			value  TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("exec DDL: %w", err)
		}
	}
	return nil
}
