package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// Database represents a pre-registered or provisioned database record.
type Database struct {
	ID            string
	ReferenceID   string
	OrgID         string
	ProjectID     string
	DBType        string
	RequestedName string
	ActualDBName  string
	Host          string
	Port          int
	Username      string
	Password      string
	Status        string // pending | provisioning | healthy | faulty
}

// DatabaseWithComponents extends Database with the list of component names
// linked to it via database_component_links.
type DatabaseWithComponents struct {
	Database
	Components []string
}

// MappingRepository persists and retrieves database records.
type MappingRepository interface {
	// RegisterDatabase creates a pending database record and a component link.
	RegisterDatabase(ctx context.Context, db *Database, componentName string) error
	// GetByReferenceID retrieves a database record by its reference_id.
	// Returns (nil, nil) when not found.
	GetByReferenceID(ctx context.Context, referenceID string) (*Database, error)
	// ActivateDatabase populates the connection credentials and sets status to provisioning.
	ActivateDatabase(ctx context.Context, id, actualDBName, host string, port int, username, password string) error
	// UpdateStatus sets the status column for a database record.
	UpdateStatus(ctx context.Context, id, status string) error
	// ListByProject retrieves all database records for a project, each with its linked components.
	ListByProject(ctx context.Context, orgID, projectID string) ([]*DatabaseWithComponents, error)
	// GetByComponent retrieves the first database linked to a component (for lookup_database MCP compat).
	// Returns (nil, nil) when not found.
	GetByComponent(ctx context.Context, orgID, projectID, componentName string) (*Database, error)
}

type mappingRepository struct {
	db *sql.DB
}

// NewMappingRepository creates a MappingRepository backed by the given *sql.DB.
func NewMappingRepository(db *sql.DB) MappingRepository {
	return &mappingRepository{db: db}
}

func (r *mappingRepository) RegisterDatabase(ctx context.Context, d *Database, componentName string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var id string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO databases (reference_id, org_id, project_id, db_type, requested_name, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')
		ON CONFLICT (reference_id) DO UPDATE SET updated_at = NOW()
		RETURNING id
	`, d.ReferenceID, d.OrgID, d.ProjectID, d.DBType, d.RequestedName).Scan(&id)
	if err != nil {
		return fmt.Errorf("insert database: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO database_component_links (database_id, org_id, project_id, component_name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (database_id, component_name) DO NOTHING
	`, id, d.OrgID, d.ProjectID, componentName)
	if err != nil {
		return fmt.Errorf("insert component link: %w", err)
	}

	return tx.Commit()
}

func (r *mappingRepository) GetByReferenceID(ctx context.Context, referenceID string) (*Database, error) {
	var d Database
	var actualDBName, host, username, password sql.NullString
	var port sql.NullInt64

	err := r.db.QueryRowContext(ctx, `
		SELECT id, reference_id, org_id, project_id, db_type, requested_name,
		       actual_db_name, host, port, username, password, status
		FROM databases
		WHERE reference_id = $1
	`, referenceID).Scan(
		&d.ID, &d.ReferenceID, &d.OrgID, &d.ProjectID, &d.DBType, &d.RequestedName,
		&actualDBName, &host, &port, &username, &password, &d.Status,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get database by reference_id: %w", err)
	}
	d.ActualDBName = actualDBName.String
	d.Host = host.String
	d.Port = int(port.Int64)
	d.Username = username.String
	d.Password = password.String
	return &d, nil
}

func (r *mappingRepository) ActivateDatabase(ctx context.Context, id, actualDBName, host string, port int, username, password string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE databases
		SET actual_db_name = $1, host = $2, port = $3, username = $4, password = $5,
		    status = 'provisioning', updated_at = NOW()
		WHERE id = $6
	`, actualDBName, host, port, username, password, id)
	if err != nil {
		return fmt.Errorf("activate database: %w", err)
	}
	return nil
}

func (r *mappingRepository) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE databases SET status = $1, updated_at = NOW() WHERE id = $2
	`, status, id)
	if err != nil {
		return fmt.Errorf("update database status: %w", err)
	}
	return nil
}

func (r *mappingRepository) ListByProject(ctx context.Context, orgID, projectID string) ([]*DatabaseWithComponents, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.id, d.reference_id, d.org_id, d.project_id, d.db_type, d.requested_name,
		       d.actual_db_name, d.host, d.port, d.username, d.password, d.status,
		       dcl.component_name
		FROM databases d
		JOIN database_component_links dcl ON d.id = dcl.database_id
		WHERE d.org_id = $1 AND d.project_id = $2
		ORDER BY d.created_at, dcl.component_name
	`, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list databases by project: %w", err)
	}
	defer rows.Close()

	// Merge rows by database ID — one database may have multiple component links.
	byID := make(map[string]*DatabaseWithComponents)
	var order []string

	for rows.Next() {
		var d Database
		var componentName string
		var actualDBName, host, username, password sql.NullString
		var port sql.NullInt64

		if err := rows.Scan(
			&d.ID, &d.ReferenceID, &d.OrgID, &d.ProjectID, &d.DBType, &d.RequestedName,
			&actualDBName, &host, &port, &username, &password, &d.Status,
			&componentName,
		); err != nil {
			return nil, fmt.Errorf("scan database row: %w", err)
		}
		d.ActualDBName = actualDBName.String
		d.Host = host.String
		d.Port = int(port.Int64)
		d.Username = username.String
		d.Password = password.String

		if _, seen := byID[d.ID]; !seen {
			entry := &DatabaseWithComponents{Database: d}
			byID[d.ID] = entry
			order = append(order, d.ID)
		}
		byID[d.ID].Components = append(byID[d.ID].Components, componentName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate database rows: %w", err)
	}

	result := make([]*DatabaseWithComponents, 0, len(order))
	for _, id := range order {
		result = append(result, byID[id])
	}
	return result, nil
}

func (r *mappingRepository) GetByComponent(ctx context.Context, orgID, projectID, componentName string) (*Database, error) {
	var d Database
	var actualDBName, host, username, password sql.NullString
	var port sql.NullInt64

	err := r.db.QueryRowContext(ctx, `
		SELECT d.id, d.reference_id, d.org_id, d.project_id, d.db_type, d.requested_name,
		       d.actual_db_name, d.host, d.port, d.username, d.password, d.status
		FROM databases d
		JOIN database_component_links dcl ON d.id = dcl.database_id
		WHERE dcl.org_id = $1 AND dcl.project_id = $2 AND dcl.component_name = $3
		ORDER BY d.created_at DESC
		LIMIT 1
	`, orgID, projectID, componentName).Scan(
		&d.ID, &d.ReferenceID, &d.OrgID, &d.ProjectID, &d.DBType, &d.RequestedName,
		&actualDBName, &host, &port, &username, &password, &d.Status,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get database by component: %w", err)
	}
	d.ActualDBName = actualDBName.String
	d.Host = host.String
	d.Port = int(port.Int64)
	d.Username = username.String
	d.Password = password.String
	return &d, nil
}
