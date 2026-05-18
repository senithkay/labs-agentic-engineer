package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// DatabaseMapping records which database was provisioned for a given
// (org, project, component) triple together with its connection credentials.
type DatabaseMapping struct {
	OrgID     string
	ProjectID string
	Component string
	DBType    string
	DBName    string
	Host      string
	Port      int
	Username  string
	Password  string
}

// MappingRepository persists and retrieves database mappings.
type MappingRepository interface {
	// Upsert stores or updates a mapping keyed on (org_id, project_id, component).
	Upsert(ctx context.Context, m *DatabaseMapping) error
	// Get retrieves the mapping for the given key. Returns (nil, nil) when not found.
	Get(ctx context.Context, orgID, projectID, component string) (*DatabaseMapping, error)
	// ListByProject retrieves all mappings for a given (org_id, project_id) pair.
	ListByProject(ctx context.Context, orgID, projectID string) ([]*DatabaseMapping, error)
}

type mappingRepository struct {
	db *sql.DB
}

// NewMappingRepository creates a MappingRepository backed by the given *sql.DB.
func NewMappingRepository(db *sql.DB) MappingRepository {
	return &mappingRepository{db: db}
}

func (r *mappingRepository) Upsert(ctx context.Context, m *DatabaseMapping) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO database_mappings
			(org_id, project_id, component, db_type, db_name, host, port, username, password, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (org_id, project_id, component) DO UPDATE SET
			db_type    = EXCLUDED.db_type,
			db_name    = EXCLUDED.db_name,
			host       = EXCLUDED.host,
			port       = EXCLUDED.port,
			username   = EXCLUDED.username,
			password   = EXCLUDED.password,
			updated_at = NOW()
	`, m.OrgID, m.ProjectID, m.Component, m.DBType, m.DBName, m.Host, m.Port, m.Username, m.Password)
	if err != nil {
		return fmt.Errorf("upsert mapping: %w", err)
	}
	return nil
}

func (r *mappingRepository) ListByProject(ctx context.Context, orgID, projectID string) ([]*DatabaseMapping, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT org_id, project_id, component, db_type, db_name, host, port, username, password
		FROM database_mappings
		WHERE org_id = $1 AND project_id = $2
		ORDER BY component
	`, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list mappings by project: %w", err)
	}
	defer rows.Close()

	var mappings []*DatabaseMapping
	for rows.Next() {
		var m DatabaseMapping
		if err := rows.Scan(
			&m.OrgID, &m.ProjectID, &m.Component,
			&m.DBType, &m.DBName,
			&m.Host, &m.Port,
			&m.Username, &m.Password,
		); err != nil {
			return nil, fmt.Errorf("scan mapping row: %w", err)
		}
		mappings = append(mappings, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mapping rows: %w", err)
	}
	return mappings, nil
}

func (r *mappingRepository) Get(ctx context.Context, orgID, projectID, component string) (*DatabaseMapping, error) {
	var m DatabaseMapping
	err := r.db.QueryRowContext(ctx, `
		SELECT org_id, project_id, component, db_type, db_name, host, port, username, password
		FROM database_mappings
		WHERE org_id = $1 AND project_id = $2 AND component = $3
	`, orgID, projectID, component).Scan(
		&m.OrgID, &m.ProjectID, &m.Component,
		&m.DBType, &m.DBName,
		&m.Host, &m.Port,
		&m.Username, &m.Password,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get mapping: %w", err)
	}
	return &m, nil
}
