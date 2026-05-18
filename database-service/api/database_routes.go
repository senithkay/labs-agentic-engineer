package api

import (
	"net/http"

	"github.com/wso2/asdlc/database-service/controllers"
)

func registerDatabaseRoutes(mux *http.ServeMux, ctrl controllers.DatabaseController) {
	mux.HandleFunc("GET /health", ctrl.HealthCheck)
	mux.HandleFunc("POST /api/v1/connections/test", ctrl.TestConnection)
	mux.HandleFunc("POST /api/v1/databases", ctrl.CreateDatabase)
	mux.HandleFunc("GET /api/v1/databases/lookup", ctrl.LookupDatabase)
	// List all databases for a project — used by the BFF artifacts panel.
	// Must be registered after /lookup to avoid the pattern matching /lookup as a project list.
	mux.HandleFunc("GET /api/v1/databases", ctrl.ListProjectDatabases)
}
