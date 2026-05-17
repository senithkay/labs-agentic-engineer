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

// CreateDatabaseRequest carries all parameters needed to provision a database
// and record the (org, project, component) → database mapping.
type CreateDatabaseRequest struct {
	DBType    DBType
	Name      string
	OrgID     string
	ProjectID string
	Component string
}

// DatabaseService manages database provisioning across MySQL and MongoDB.
type DatabaseService interface {
	// HealthCheck returns the connectivity status of all database engines.
	HealthCheck(ctx context.Context) *HealthStatus
	// TestProvisionedDatabase looks up the credentials for the given (org, project, component)
	// key and verifies the connection using those per-database user credentials — not root.
	TestProvisionedDatabase(ctx context.Context, orgID, projectID, component string) error
	// CreateDatabase creates a new database and a dedicated user on the given engine,
	// then records the (org, project, component) → database mapping.
	CreateDatabase(ctx context.Context, req CreateDatabaseRequest) (*DatabaseCredentials, error)
	// LookupDatabase retrieves the database credentials for the given (org, project, component) key.
	// Returns (nil, nil) when no mapping exists.
	LookupDatabase(ctx context.Context, orgID, projectID, component string) (*DatabaseCredentials, error)
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

func (s *databaseService) TestProvisionedDatabase(ctx context.Context, orgID, projectID, component string) error {
	creds, err := s.LookupDatabase(ctx, orgID, projectID, component)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	if creds == nil {
		return fmt.Errorf("no database found for org=%s project=%s component=%s", orgID, projectID, component)
	}

	switch DBType(creds.DBType) {
	case DBTypeMySQL:
		return s.mysql.testCredentials(ctx, creds.Host, creds.Port, creds.Database, creds.Username, creds.Password)
	case DBTypeMongoDB:
		return s.mongodb.testCredentials(ctx, creds.Host, creds.Port, creds.Database, creds.Username, creds.Password)
	default:
		return fmt.Errorf("unknown database type: %s", creds.DBType)
	}
}

func (s *databaseService) CreateDatabase(ctx context.Context, req CreateDatabaseRequest) (*DatabaseCredentials, error) {
	var (
		creds *DatabaseCredentials
		err   error
	)

	switch req.DBType {
	case DBTypeMySQL:
		creds, err = s.mysql.createDatabase(ctx, req.Name)
	case DBTypeMongoDB:
		creds, err = s.mongodb.createDatabase(ctx, req.Name)
	default:
		return nil, fmt.Errorf("unknown database type: %s", req.DBType)
	}
	if err != nil {
		return nil, err
	}

	// Persist the (org, project, component) → database mapping.
	if req.OrgID != "" && req.ProjectID != "" && req.Component != "" {
		mapping := &repository.DatabaseMapping{
			OrgID:     req.OrgID,
			ProjectID: req.ProjectID,
			Component: req.Component,
			DBType:    creds.DBType,
			DBName:    creds.Database,
			Host:      creds.Host,
			Port:      creds.Port,
			Username:  creds.Username,
			Password:  creds.Password,
		}
		if upsertErr := s.mappingRepo.Upsert(ctx, mapping); upsertErr != nil {
			// Log but do not fail the operation — the database was created successfully.
			slog.ErrorContext(ctx, "failed to store database mapping",
				"org_id", req.OrgID,
				"project_id", req.ProjectID,
				"component", req.Component,
				"error", upsertErr,
			)
		}
	}

	return creds, nil
}

func (s *databaseService) LookupDatabase(ctx context.Context, orgID, projectID, component string) (*DatabaseCredentials, error) {
	mapping, err := s.mappingRepo.Get(ctx, orgID, projectID, component)
	if err != nil {
		return nil, fmt.Errorf("lookup database: %w", err)
	}
	if mapping == nil {
		return nil, nil // not found
	}
	return &DatabaseCredentials{
		DBType:   mapping.DBType,
		Host:     mapping.Host,
		Port:     mapping.Port,
		Database: mapping.DBName,
		Username: mapping.Username,
		Password: mapping.Password,
	}, nil
}
