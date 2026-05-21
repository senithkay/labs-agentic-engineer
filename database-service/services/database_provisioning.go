package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wso2/asdlc/database-service/repository"
)

// DBType identifies the database engine.
type DBType string

const (
	DBTypeMySQL   DBType = "mysql"
	DBTypeMongoDB DBType = "mongodb"
)

// DBStatus holds the connectivity status for a single database engine.
type DBStatus struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// HealthStatus holds the health of all database engines.
type HealthStatus struct {
	MySQL   DBStatus `json:"mysql"`
	MongoDB DBStatus `json:"mongodb"`
}

// DatabaseCredentials holds the connection details for a provisioned database.
type DatabaseCredentials struct {
	DBType   string `json:"db_type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// RegisterDatabaseRequest carries parameters for pre-registering a database.
type RegisterDatabaseRequest struct {
	ReferenceID   string // task ID — unique globally
	OrgID         string
	ProjectID     string
	DBType        DBType
	RequestedName string
	ComponentName string
}

// CreateDatabaseRequest carries parameters for provisioning a pre-registered database.
type CreateDatabaseRequest struct {
	ReferenceID string
	OrgID       string
	ProjectID   string
}

// DatabaseRecord is the service-layer view of a databases row + its linked components.
type DatabaseRecord struct {
	ID            string   `json:"id"`
	ReferenceID   string   `json:"referenceId"`
	OrgID         string   `json:"orgId"`
	ProjectID     string   `json:"projectId"`
	DBType        string   `json:"dbType"`
	RequestedName string   `json:"requestedName"`
	ActualDBName  string   `json:"actualDbName,omitempty"`
	Host          string   `json:"host,omitempty"`
	Port          int      `json:"port,omitempty"`
	Username      string   `json:"username,omitempty"`
	Password      string   `json:"password,omitempty"`
	Status        string   `json:"status"`
	Components    []string `json:"components,omitempty"`
}

// DatabaseArtifact is a project-level view returned to the BFF for the console panel.
type DatabaseArtifact struct {
	ID            string   `json:"id"`
	ReferenceID   string   `json:"referenceId"`
	Components    []string `json:"components"`
	DBType        string   `json:"dbType"`
	RequestedName string   `json:"requestedName"`
	ActualDBName  string   `json:"actualDbName,omitempty"`
	Host          string   `json:"host,omitempty"`
	Port          int      `json:"port,omitempty"`
	Username      string   `json:"username,omitempty"`
	Password      string   `json:"password,omitempty"`
	Status        string   `json:"status"`
}

// DatabaseService manages database provisioning across MySQL and MongoDB.
type DatabaseService interface {
	// HealthCheck returns the connectivity status of all database engines.
	HealthCheck(ctx context.Context) *HealthStatus
	// RegisterDatabase pre-registers a database in pending state. Idempotent on reference_id.
	RegisterDatabase(ctx context.Context, req RegisterDatabaseRequest) error
	// GetPendingDatabase retrieves the pending database record identified by reference_id.
	// Returns (nil, nil) when not found.
	GetPendingDatabase(ctx context.Context, referenceID string) (*DatabaseRecord, error)
	// CreateDatabase provisions the pre-registered database in the appropriate engine,
	// stores credentials, and sets status to provisioning.
	CreateDatabase(ctx context.Context, req CreateDatabaseRequest) (*DatabaseCredentials, error)
	// TestDatabase tests the connection for the database identified by reference_id,
	// updates the status to "healthy" or "faulty", and returns the status string.
	TestDatabase(ctx context.Context, referenceID string) (string, error)
	// LookupDatabase retrieves credentials by (org, project, component) for use by
	// service components that depend on this database.
	// Returns (nil, nil) when not found.
	LookupDatabase(ctx context.Context, orgID, projectID, component string) (*DatabaseCredentials, error)
	// ListProjectDatabases returns all database artifacts for a project.
	ListProjectDatabases(ctx context.Context, orgID, projectID string) ([]*DatabaseArtifact, error)
	// UpdateDatabaseStatusByRef sets the status of the database identified by reference_id.
	UpdateDatabaseStatusByRef(ctx context.Context, referenceID, status string) error
}

type databaseService struct {
	mysql       *mysqlBackend
	mongodb     *mongoDBBackend
	mappingRepo repository.MappingRepository
}

// NewDatabaseService creates a DatabaseService backed by MySQL, MongoDB, and
// an internal PostgreSQL mapping store.
func NewDatabaseService(
	mysqlRootURL, mysqlHost string, mysqlPort int,
	mongoRootURL, mongoHost string, mongoPort int,
	mappingRepo repository.MappingRepository,
) DatabaseService {
	return &databaseService{
		mysql:       newMySQLBackend(mysqlRootURL, mysqlHost, mysqlPort),
		mongodb:     newMongoDBBackend(mongoRootURL, mongoHost, mongoPort),
		mappingRepo: mappingRepo,
	}
}

func (s *databaseService) HealthCheck(ctx context.Context) *HealthStatus {
	status := &HealthStatus{}

	if err := s.mysql.ping(ctx); err != nil {
		status.MySQL = DBStatus{OK: false, Message: err.Error()}
	} else {
		status.MySQL = DBStatus{OK: true}
	}

	if err := s.mongodb.ping(ctx); err != nil {
		status.MongoDB = DBStatus{OK: false, Message: err.Error()}
	} else {
		status.MongoDB = DBStatus{OK: true}
	}

	return status
}

func (s *databaseService) RegisterDatabase(ctx context.Context, req RegisterDatabaseRequest) error {
	d := &repository.Database{
		ReferenceID:   req.ReferenceID,
		OrgID:         req.OrgID,
		ProjectID:     req.ProjectID,
		DBType:        string(req.DBType),
		RequestedName: req.RequestedName,
	}
	if err := s.mappingRepo.RegisterDatabase(ctx, d, req.ComponentName); err != nil {
		return fmt.Errorf("register database: %w", err)
	}
	slog.InfoContext(ctx, "database pre-registered",
		"reference_id", req.ReferenceID, "component", req.ComponentName, "db_type", req.DBType)
	return nil
}

func (s *databaseService) GetPendingDatabase(ctx context.Context, referenceID string) (*DatabaseRecord, error) {
	d, err := s.mappingRepo.GetByReferenceID(ctx, referenceID)
	if err != nil {
		return nil, fmt.Errorf("get pending database: %w", err)
	}
	if d == nil {
		return nil, nil
	}
	return repoToRecord(d, nil), nil
}

func (s *databaseService) CreateDatabase(ctx context.Context, req CreateDatabaseRequest) (*DatabaseCredentials, error) {
	d, err := s.mappingRepo.GetByReferenceID(ctx, req.ReferenceID)
	if err != nil {
		return nil, fmt.Errorf("lookup pending database: %w", err)
	}
	if d == nil {
		return nil, fmt.Errorf("no pending database found for reference_id=%s", req.ReferenceID)
	}

	var creds *DatabaseCredentials
	switch DBType(d.DBType) {
	case DBTypeMySQL:
		creds, err = s.mysql.createDatabase(ctx, d.RequestedName)
	case DBTypeMongoDB:
		creds, err = s.mongodb.createDatabase(ctx, d.RequestedName)
	default:
		return nil, fmt.Errorf("unknown database type: %s", d.DBType)
	}
	if err != nil {
		if updateErr := s.mappingRepo.UpdateStatus(ctx, d.ID, "faulty"); updateErr != nil {
			slog.ErrorContext(ctx, "failed to mark database faulty after creation error",
				"reference_id", req.ReferenceID, "error", updateErr)
		}
		return nil, err
	}

	if activateErr := s.mappingRepo.ActivateDatabase(ctx, d.ID, creds.Database, creds.Host, creds.Port, creds.Username, creds.Password); activateErr != nil {
		slog.ErrorContext(ctx, "failed to activate database record",
			"reference_id", req.ReferenceID, "error", activateErr)
	}

	return creds, nil
}

func (s *databaseService) TestDatabase(ctx context.Context, referenceID string) (string, error) {
	d, err := s.mappingRepo.GetByReferenceID(ctx, referenceID)
	if err != nil {
		return "", fmt.Errorf("lookup database: %w", err)
	}
	if d == nil {
		return "", fmt.Errorf("no database found for reference_id=%s", referenceID)
	}
	if d.Host == "" || d.ActualDBName == "" {
		return "", fmt.Errorf("database has not been provisioned yet (reference_id=%s)", referenceID)
	}

	var testErr error
	switch DBType(d.DBType) {
	case DBTypeMySQL:
		testErr = s.mysql.testCredentials(ctx, d.Host, d.Port, d.ActualDBName, d.Username, d.Password)
	case DBTypeMongoDB:
		testErr = s.mongodb.testCredentials(ctx, d.Host, d.Port, d.ActualDBName, d.Username, d.Password)
	default:
		return "", fmt.Errorf("unknown database type: %s", d.DBType)
	}

	status := "healthy"
	if testErr != nil {
		status = "faulty"
		slog.WarnContext(ctx, "database connection test failed",
			"reference_id", referenceID, "error", testErr)
	}

	if updateErr := s.mappingRepo.UpdateStatus(ctx, d.ID, status); updateErr != nil {
		slog.ErrorContext(ctx, "failed to update database status",
			"reference_id", referenceID, "status", status, "error", updateErr)
	}

	return status, nil
}

func (s *databaseService) LookupDatabase(ctx context.Context, orgID, projectID, component string) (*DatabaseCredentials, error) {
	d, err := s.mappingRepo.GetByComponent(ctx, orgID, projectID, component)
	if err != nil {
		return nil, fmt.Errorf("lookup database: %w", err)
	}
	if d == nil {
		return nil, nil
	}
	if d.Host == "" {
		return nil, nil // pre-registered but not yet provisioned
	}
	return &DatabaseCredentials{
		DBType:   d.DBType,
		Host:     d.Host,
		Port:     d.Port,
		Database: d.ActualDBName,
		Username: d.Username,
		Password: d.Password,
	}, nil
}

func (s *databaseService) ListProjectDatabases(ctx context.Context, orgID, projectID string) ([]*DatabaseArtifact, error) {
	rows, err := s.mappingRepo.ListByProject(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project databases: %w", err)
	}
	artifacts := make([]*DatabaseArtifact, 0, len(rows))
	for _, r := range rows {
		artifacts = append(artifacts, &DatabaseArtifact{
			ID:            r.ID,
			ReferenceID:   r.ReferenceID,
			Components:    r.Components,
			DBType:        r.DBType,
			RequestedName: r.RequestedName,
			ActualDBName:  r.ActualDBName,
			Host:          r.Host,
			Port:          r.Port,
			Username:      r.Username,
			Password:      r.Password,
			Status:        r.Status,
		})
	}
	return artifacts, nil
}

func (s *databaseService) UpdateDatabaseStatusByRef(ctx context.Context, referenceID, status string) error {
	d, err := s.mappingRepo.GetByReferenceID(ctx, referenceID)
	if err != nil {
		return fmt.Errorf("lookup database: %w", err)
	}
	if d == nil {
		return fmt.Errorf("no database found for reference_id=%s", referenceID)
	}
	if err := s.mappingRepo.UpdateStatus(ctx, d.ID, status); err != nil {
		return fmt.Errorf("update database status: %w", err)
	}
	slog.InfoContext(ctx, "database status updated", "reference_id", referenceID, "status", status)
	return nil
}

func repoToRecord(d *repository.Database, components []string) *DatabaseRecord {
	return &DatabaseRecord{
		ID:            d.ID,
		ReferenceID:   d.ReferenceID,
		OrgID:         d.OrgID,
		ProjectID:     d.ProjectID,
		DBType:        d.DBType,
		RequestedName: d.RequestedName,
		ActualDBName:  d.ActualDBName,
		Host:          d.Host,
		Port:          d.Port,
		Username:      d.Username,
		Password:      d.Password,
		Status:        d.Status,
		Components:    components,
	}
}
