// Package store provides SQLite-backed persistence for garm-gitlab instance state.
//
// On startup the pool manager loads all previously known instances from the
// store so that a restart doesn't orphan running Incus containers. On every
// state change (create, status update, delete) the store is updated
// synchronously before the in-memory map is modified, so the two are always
// consistent.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite, no cgo
)

// Instance mirrors pool.Instance for persistence purposes.
// It is defined here (not in pool) to avoid an import cycle.
type Instance struct {
	ID          string
	RunnerID    int64
	RunnerToken string
	PoolID      string
	CreatedAt   time.Time
	LastJobAt   time.Time
	Status      string
}

// Store is a SQLite-backed instance registry.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping store %s: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate store: %w", err)
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// SaveInstance inserts or replaces an instance record.
func (s *Store) SaveInstance(inst Instance) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO instances
			(id, runner_id, runner_token, pool_id, created_at, last_job_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		inst.ID,
		inst.RunnerID,
		inst.RunnerToken,
		inst.PoolID,
		inst.CreatedAt.UTC().Format(time.RFC3339),
		inst.LastJobAt.UTC().Format(time.RFC3339),
		inst.Status,
	)
	if err != nil {
		return fmt.Errorf("save instance %s: %w", inst.ID, err)
	}
	return nil
}

// DeleteInstance removes an instance record by ID.
func (s *Store) DeleteInstance(id string) error {
	_, err := s.db.Exec(`DELETE FROM instances WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete instance %s: %w", id, err)
	}
	return nil
}

// UpdateStatus updates the status field of an existing instance.
func (s *Store) UpdateStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE instances SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("update status %s: %w", id, err)
	}
	return nil
}

// UpdateLastJobAt updates the last_job_at timestamp for an instance.
func (s *Store) UpdateLastJobAt(id string, t time.Time) error {
	_, err := s.db.Exec(
		`UPDATE instances SET last_job_at = ? WHERE id = ?`,
		t.UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("update last_job_at %s: %w", id, err)
	}
	return nil
}

// ListByPool returns all instances belonging to poolID.
func (s *Store) ListByPool(poolID string) ([]Instance, error) {
	rows, err := s.db.Query(
		`SELECT id, runner_id, runner_token, pool_id, created_at, last_job_at, status
		 FROM instances WHERE pool_id = ?`, poolID,
	)
	if err != nil {
		return nil, fmt.Errorf("list instances for pool %s: %w", poolID, err)
	}
	defer rows.Close()

	var out []Instance
	for rows.Next() {
		var inst Instance
		var createdAt, lastJobAt string
		if err := rows.Scan(
			&inst.ID, &inst.RunnerID, &inst.RunnerToken, &inst.PoolID,
			&createdAt, &lastJobAt, &inst.Status,
		); err != nil {
			return nil, err
		}
		inst.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		inst.LastJobAt, _ = time.Parse(time.RFC3339, lastJobAt)
		out = append(out, inst)
	}
	return out, rows.Err()
}

// ListAll returns every instance across all pools.
func (s *Store) ListAll() ([]Instance, error) {
	rows, err := s.db.Query(
		`SELECT id, runner_id, runner_token, pool_id, created_at, last_job_at, status
		 FROM instances ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all instances: %w", err)
	}
	defer rows.Close()

	var out []Instance
	for rows.Next() {
		var inst Instance
		var createdAt, lastJobAt string
		if err := rows.Scan(
			&inst.ID, &inst.RunnerID, &inst.RunnerToken, &inst.PoolID,
			&createdAt, &lastJobAt, &inst.Status,
		); err != nil {
			return nil, err
		}
		inst.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		inst.LastJobAt, _ = time.Parse(time.RFC3339, lastJobAt)
		out = append(out, inst)
	}
	return out, rows.Err()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS instances (
			id           TEXT PRIMARY KEY,
			runner_id    INTEGER NOT NULL,
			runner_token TEXT    NOT NULL,
			pool_id      TEXT    NOT NULL,
			created_at   TEXT    NOT NULL,
			last_job_at  TEXT    NOT NULL,
			status       TEXT    NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_instances_pool ON instances(pool_id);
	`)
	return err
}
