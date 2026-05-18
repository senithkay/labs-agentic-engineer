package api

import (
	"net/http"

	"github.com/wso2/asdlc/database-service/controllers"
)

func registerDatabaseRoutes(mux *http.ServeMux, ctrl controllers.DatabaseController) {
	mux.HandleFunc("GET /health", ctrl.HealthCheck)
	mux.HandleFunc("POST /api/v1/databases/register", ctrl.RegisterDatabase)
	mux.HandleFunc("GET /api/v1/databases", ctrl.ListProjectDatabases)
	mux.HandleFunc("PATCH /api/v1/databases/{referenceId}/status", ctrl.UpdateDatabaseStatus)
}
