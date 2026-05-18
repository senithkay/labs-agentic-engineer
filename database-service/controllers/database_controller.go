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
	RegisterDatabase(w http.ResponseWriter, r *http.Request)
	ListProjectDatabases(w http.ResponseWriter, r *http.Request)
	UpdateDatabaseStatus(w http.ResponseWriter, r *http.Request)
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

type registerDatabaseRequest struct {
	ReferenceID   string `json:"referenceId"`
	OrgID         string `json:"orgId"`
	ProjectID     string `json:"projectId"`
	DBType        string `json:"dbType"`
	RequestedName string `json:"requestedName"`
	ComponentName string `json:"componentName"`
}

// RegisterDatabase handles POST /api/v1/databases/register.
// Pre-registers a database in pending state. Called by the BFF after task generation.
func (c *databaseController) RegisterDatabase(w http.ResponseWriter, r *http.Request) {
	var req registerDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ReferenceID == "" || req.OrgID == "" || req.ProjectID == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "referenceId, orgId, and projectId are required")
		return
	}
	if req.DBType != string(services.DBTypeMySQL) && req.DBType != string(services.DBTypeMongoDB) {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "dbType must be 'mysql' or 'mongodb'")
		return
	}
	if req.RequestedName == "" || req.ComponentName == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "requestedName and componentName are required")
		return
	}

	if err := c.svc.RegisterDatabase(r.Context(), services.RegisterDatabaseRequest{
		ReferenceID:   req.ReferenceID,
		OrgID:         req.OrgID,
		ProjectID:     req.ProjectID,
		DBType:        services.DBType(req.DBType),
		RequestedName: req.RequestedName,
		ComponentName: req.ComponentName,
	}); err != nil {
		slog.ErrorContext(r.Context(), "failed to register database",
			"reference_id", req.ReferenceID, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to register database")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "pending"}) //nolint:errcheck
}

// UpdateDatabaseStatus handles PATCH /api/v1/databases/{referenceId}/status.
// Called by the BFF to drive status transitions (e.g. pending → provisioning on dispatch).
func (c *databaseController) UpdateDatabaseStatus(w http.ResponseWriter, r *http.Request) {
	referenceID := r.PathValue("referenceId")
	if referenceID == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "referenceId path param is required")
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch body.Status {
	case "pending", "provisioning", "healthy", "faulty":
		// valid
	default:
		utils.WriteErrorResponse(w, http.StatusBadRequest, "status must be one of: pending, provisioning, healthy, faulty")
		return
	}

	if err := c.svc.UpdateDatabaseStatusByRef(r.Context(), referenceID, body.Status); err != nil {
		slog.ErrorContext(r.Context(), "failed to update database status",
			"reference_id", referenceID, "status", body.Status, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to update database status")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": body.Status}) //nolint:errcheck
}

// ListProjectDatabases handles GET /api/v1/databases?org_id=&project_id=
// Returns all database artifacts for a project, including status from the mapping table.
func (c *databaseController) ListProjectDatabases(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	projectID := r.URL.Query().Get("project_id")

	if orgID == "" || projectID == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "org_id and project_id query params are required")
		return
	}

	artifacts, err := c.svc.ListProjectDatabases(r.Context(), orgID, projectID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list project databases",
			"org_id", orgID, "project_id", projectID, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to list project databases")
		return
	}

	type response struct {
		Databases []*services.DatabaseArtifact `json:"databases"`
	}
	if artifacts == nil {
		artifacts = []*services.DatabaseArtifact{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response{Databases: artifacts}) //nolint:errcheck
}
