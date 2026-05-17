package controllers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wso2/asdlc/database-service/services"
	"github.com/wso2/asdlc/database-service/utils"
)

// DatabaseController handles HTTP requests for database operations.
type DatabaseController interface {
	HealthCheck(w http.ResponseWriter, r *http.Request)
	TestConnection(w http.ResponseWriter, r *http.Request)
	CreateDatabase(w http.ResponseWriter, r *http.Request)
	LookupDatabase(w http.ResponseWriter, r *http.Request)
}

type databaseController struct {
	svc services.DatabaseService
}

func NewDatabaseController(svc services.DatabaseService) DatabaseController {
	return &databaseController{svc: svc}
}

type healthResponse struct {
	Status  string            `json:"status"`
	MySQL   services.DBStatus `json:"mysql"`
	MongoDB services.DBStatus `json:"mongodb"`
}

// HealthCheck handles GET /health.
// Always returns 200; callers should inspect the per-engine ok fields.
func (c *databaseController) HealthCheck(w http.ResponseWriter, r *http.Request) {
	status := c.svc.HealthCheck(r.Context())

	overall := "ok"
	if !status.MySQL.OK || !status.MongoDB.OK {
		overall = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(healthResponse{ //nolint:errcheck
		Status:  overall,
		MySQL:   status.MySQL,
		MongoDB: status.MongoDB,
	})
}

type testConnectionRequest struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Component string `json:"component"`
}

type testConnectionResponse struct {
	Status    string `json:"status"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Component string `json:"component"`
	Error     string `json:"error,omitempty"`
}

// TestConnection handles POST /api/v1/connections/test.
// Looks up the provisioned credentials for the given (org, project, component) and
// verifies the connection using those per-database user credentials — not root.
func (c *databaseController) TestConnection(w http.ResponseWriter, r *http.Request) {
	var req testConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.OrgID == "" || req.ProjectID == "" || req.Component == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "org_id, project_id, and component are required")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := c.svc.TestProvisionedDatabase(r.Context(), req.OrgID, req.ProjectID, req.Component); err != nil {
		slog.DebugContext(r.Context(), "provisioned database connection test failed",
			"org_id", req.OrgID, "project_id", req.ProjectID, "component", req.Component, "error", err)
		json.NewEncoder(w).Encode(testConnectionResponse{ //nolint:errcheck
			Status:    "failed",
			OrgID:     req.OrgID,
			ProjectID: req.ProjectID,
			Component: req.Component,
			Error:     err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(testConnectionResponse{ //nolint:errcheck
		Status:    "ok",
		OrgID:     req.OrgID,
		ProjectID: req.ProjectID,
		Component: req.Component,
	})
}

type createDatabaseRequest struct {
	DBType    string `json:"db_type"`
	Name      string `json:"name"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Component string `json:"component"`
}

// CreateDatabase handles POST /api/v1/databases.
// The caller provides the database name and the (org, project, component) key;
// the service manages credentials and records the mapping.
func (c *databaseController) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	var req createDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DBType != string(services.DBTypeMySQL) && req.DBType != string(services.DBTypeMongoDB) {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "db_type must be 'mysql' or 'mongodb'")
		return
	}
	if req.Name == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.OrgID == "" || req.ProjectID == "" || req.Component == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "org_id, project_id, and component are required")
		return
	}

	creds, err := c.svc.CreateDatabase(r.Context(), services.CreateDatabaseRequest{
		DBType:    services.DBType(req.DBType),
		Name:      req.Name,
		OrgID:     req.OrgID,
		ProjectID: req.ProjectID,
		Component: req.Component,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to create database",
			"db_type", req.DBType, "name", req.Name, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to create database")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(creds) //nolint:errcheck
}

// LookupDatabase handles GET /api/v1/databases/lookup?org_id=&project_id=&component=
func (c *databaseController) LookupDatabase(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	projectID := r.URL.Query().Get("project_id")
	component := r.URL.Query().Get("component")

	if orgID == "" || projectID == "" || component == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "org_id, project_id, and component query params are required")
		return
	}

	creds, err := c.svc.LookupDatabase(r.Context(), orgID, projectID, component)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to lookup database",
			"org_id", orgID, "project_id", projectID, "component", component, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to lookup database")
		return
	}
	if creds == nil {
		utils.WriteErrorResponse(w, http.StatusNotFound, "no database found for the given org, project, and component")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(creds) //nolint:errcheck
}
