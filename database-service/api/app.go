package api

import (
	"net/http"

	"github.com/wso2/asdlc/database-service/controllers"
	"github.com/wso2/asdlc/database-service/mcp"
	"github.com/wso2/asdlc/database-service/middleware"
	"github.com/wso2/asdlc/database-service/middleware/logger"
	"github.com/wso2/asdlc/database-service/services"
)

// AppParams holds all dependencies needed to build the HTTP handler.
type AppParams struct {
	DatabaseCtrl controllers.DatabaseController
	RegistryCtrl controllers.RegistryController
	DatabaseSvc  services.DatabaseService
}

// NewHandler assembles the full HTTP handler with middleware, REST routes, and the MCP server.
func NewHandler(params AppParams) http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})

	// REST routes (database operations + registry)
	registerDatabaseRoutes(mux, params.DatabaseCtrl, params.RegistryCtrl)

	// MCP server — AI agents call database tools here without knowing credentials.
	if params.DatabaseSvc != nil {
		mux.Handle("/mcp", mcp.NewServer(params.DatabaseSvc))
	}

	// Global middleware stack (outermost applied last)
	var handler http.Handler = mux
	handler = logger.RequestLogger()(handler)
	handler = middleware.RecovererOnPanic()(handler)

	return handler
}
