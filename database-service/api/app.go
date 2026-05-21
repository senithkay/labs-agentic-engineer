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
	DatabaseSvc  services.DatabaseService
}

// NewHandler assembles the full HTTP handler with middleware, REST routes, and the MCP server.
func NewHandler(params AppParams) http.Handler {
	mux := http.NewServeMux()

	// REST routes (health + database operations)
	registerDatabaseRoutes(mux, params.DatabaseCtrl)

	// MCP server — AI agents call database tools here without knowing credentials.
	mux.Handle("/mcp", mcp.NewServer(params.DatabaseSvc))

	// Global middleware stack (outermost applied last)
	var handler http.Handler = mux
	handler = logger.RequestLogger()(handler)
	handler = middleware.RecovererOnPanic()(handler)

	return handler
}
