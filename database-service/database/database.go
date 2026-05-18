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

// migrate creates the databases and database_component_links tables.
// Drops the old database_mappings table if it exists.
func migrate(db *sql.DB) error {
	// Drop old table from previous schema.
	if _, err := db.Exec(`DROP TABLE IF EXISTS database_mappings`); err != nil {
		return fmt.Errorf("drop old table: %w", err)
	}

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS databases (
			id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			reference_id   TEXT NOT NULL UNIQUE,
			org_id         TEXT NOT NULL,
			project_id     TEXT NOT NULL,
			db_type        TEXT NOT NULL,
			requested_name TEXT NOT NULL,
			actual_db_name TEXT,
			host           TEXT,
			port           INTEGER,
			username       TEXT,
			password       TEXT,
			status         TEXT NOT NULL DEFAULT 'pending',
			created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS database_component_links (
			id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			database_id    UUID NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
			org_id         TEXT NOT NULL,
			project_id     TEXT NOT NULL,
			component_name TEXT NOT NULL,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (database_id, component_name)
		);
	`)
	return err
}
