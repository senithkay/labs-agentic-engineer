package database

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// Open connects to the PostgreSQL database and runs schema migrations.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	slog.Info("internal database connected and migrated")
	return db, nil
}

// migrate creates the database_mappings table if it does not already exist.
func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS database_mappings (
			org_id     TEXT NOT NULL,
			project_id TEXT NOT NULL,
			component  TEXT NOT NULL,
			db_type    TEXT NOT NULL,
			db_name    TEXT NOT NULL,
			host       TEXT NOT NULL,
			port       INTEGER NOT NULL,
			username   TEXT NOT NULL,
			password   TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (org_id, project_id, component)
		)
	`)
	return err
}
