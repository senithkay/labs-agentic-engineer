// Package mcp implements an MCP (Model Context Protocol) server over the
// Streamable HTTP transport. AI agents call tools here without needing to
// know database credentials — the service uses its own internal connections.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wso2/asdlc/database-service/services"
)

// Server is an http.Handler implementing the MCP streamable HTTP transport.
type Server struct {
	svc services.DatabaseService
}

// NewServer creates an MCP server backed by the given database service.
func NewServer(svc services.DatabaseService) *Server {
	return &Server{svc: svc}
}

// rpcRequest is a JSON-RPC 2.0 request.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, nil, -32700, "parse error")
		return
	}

	slog.DebugContext(r.Context(), "mcp request", "method", req.Method)

	switch req.Method {
	case "initialize":
		s.handleInitialize(w, req)
	case "notifications/initialized":
		// Notification — no response body.
		w.WriteHeader(http.StatusAccepted)
	case "ping":
		writeResult(w, req.ID, struct{}{})
	case "tools/list":
		s.handleToolsList(w, req)
	case "tools/call":
		s.handleToolsCall(w, r.Context(), req)
	default:
		writeError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, req rpcRequest) {
	writeResult(w, req.ID, map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "database-service",
			"version": "1.0.0",
		},
	})
}

func (s *Server) handleToolsList(w http.ResponseWriter, req rpcRequest) {
	tools := []map[string]any{
		{
			"name":        "health_check",
			"description": "Check the health of the database service and its connectivity to MySQL and MongoDB. No parameters required.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "test_connection",
			"description": "Verify that the database provisioned for a given org/project/component is reachable. Looks up the stored credentials and tests the connection using the per-database user — not root. Use this to confirm a provisioned database is still accessible.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"org_id": map[string]any{
						"type":        "string",
						"description": "Organization identifier",
					},
					"project_id": map[string]any{
						"type":        "string",
						"description": "Project identifier",
					},
					"component": map[string]any{
						"type":        "string",
						"description": "Component identifier within the project",
					},
				},
				"required": []string{"org_id", "project_id", "component"},
			},
		},
		{
			"name":        "create_database",
			"description": "Create a new database on the specified engine for a given org/project/component. The agent supplies the database name and the org, project, and component identifiers. The service manages credentials internally and records the mapping so it can be looked up later.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"db_type": map[string]any{
						"type":        "string",
						"description": "Database engine: 'mysql' or 'mongodb'",
						"enum":        []string{"mysql", "mongodb"},
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Name of the database to create (e.g. 'my_project_db')",
					},
					"org_id": map[string]any{
						"type":        "string",
						"description": "Organization identifier",
					},
					"project_id": map[string]any{
						"type":        "string",
						"description": "Project identifier",
					},
					"component": map[string]any{
						"type":        "string",
						"description": "Component identifier within the project",
					},
				},
				"required": []string{"db_type", "name", "org_id", "project_id", "component"},
			},
		},
		{
			"name":        "lookup_database",
			"description": "Retrieve the database name, type, and connection credentials for a previously provisioned database identified by org, project, and component. Credentials are returned directly so the caller can connect without knowing them in advance.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"org_id": map[string]any{
						"type":        "string",
						"description": "Organization identifier",
					},
					"project_id": map[string]any{
						"type":        "string",
						"description": "Project identifier",
					},
					"component": map[string]any{
						"type":        "string",
						"description": "Component identifier within the project",
					},
				},
				"required": []string{"org_id", "project_id", "component"},
			},
		},
	}
	writeResult(w, req.ID, map[string]any{"tools": tools})
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleToolsCall(w http.ResponseWriter, ctx context.Context, req rpcRequest) {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(w, req.ID, -32602, "invalid params")
		return
	}

	switch p.Name {
	case "health_check":
		s.toolHealthCheck(w, ctx, req.ID)
	case "test_connection":
		s.toolTestConnection(w, ctx, req.ID, p.Arguments)
	case "create_database":
		s.toolCreateDatabase(w, ctx, req.ID, p.Arguments)
	case "lookup_database":
		s.toolLookupDatabase(w, ctx, req.ID, p.Arguments)
	default:
		writeError(w, req.ID, -32602, "unknown tool: "+p.Name)
	}
}

func (s *Server) toolHealthCheck(w http.ResponseWriter, ctx context.Context, id json.RawMessage) {
	status := s.svc.HealthCheck(ctx)
	text := fmt.Sprintf(
		"MySQL: ok=%v%s\nMongoDB: ok=%v%s",
		status.MySQL.OK, msgSuffix(status.MySQL.Message),
		status.MongoDB.OK, msgSuffix(status.MongoDB.Message),
	)
	writeToolResult(w, id, text, false)
}

func (s *Server) toolTestConnection(w http.ResponseWriter, ctx context.Context, id json.RawMessage, args json.RawMessage) {
	var a struct {
		OrgID     string `json:"org_id"`
		ProjectID string `json:"project_id"`
		Component string `json:"component"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeToolResult(w, id, "invalid arguments", true)
		return
	}
	if a.OrgID == "" || a.ProjectID == "" || a.Component == "" {
		writeToolResult(w, id, "org_id, project_id, and component are required", true)
		return
	}

	if err := s.svc.TestProvisionedDatabase(ctx, a.OrgID, a.ProjectID, a.Component); err != nil {
		writeToolResult(w, id, fmt.Sprintf("connection test failed: %v", err), true)
		return
	}

	writeToolResult(w, id, fmt.Sprintf(
		"connection successful for org=%s project=%s component=%s",
		a.OrgID, a.ProjectID, a.Component,
	), false)
}

func (s *Server) toolCreateDatabase(w http.ResponseWriter, ctx context.Context, id json.RawMessage, args json.RawMessage) {
	var a struct {
		DBType    string `json:"db_type"`
		Name      string `json:"name"`
		OrgID     string `json:"org_id"`
		ProjectID string `json:"project_id"`
		Component string `json:"component"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeToolResult(w, id, "invalid arguments", true)
		return
	}
	if a.DBType != "mysql" && a.DBType != "mongodb" {
		writeToolResult(w, id, "db_type must be 'mysql' or 'mongodb'", true)
		return
	}
	if a.Name == "" {
		writeToolResult(w, id, "name is required", true)
		return
	}
	if a.OrgID == "" || a.ProjectID == "" || a.Component == "" {
		writeToolResult(w, id, "org_id, project_id, and component are required", true)
		return
	}

	creds, err := s.svc.CreateDatabase(ctx, services.CreateDatabaseRequest{
		DBType:    services.DBType(a.DBType),
		Name:      a.Name,
		OrgID:     a.OrgID,
		ProjectID: a.ProjectID,
		Component: a.Component,
	})
	if err != nil {
		writeToolResult(w, id, fmt.Sprintf("failed to create database: %v", err), true)
		return
	}

	out, _ := json.MarshalIndent(creds, "", "  ")
	writeToolResult(w, id, string(out), false)
}

func (s *Server) toolLookupDatabase(w http.ResponseWriter, ctx context.Context, id json.RawMessage, args json.RawMessage) {
	var a struct {
		OrgID     string `json:"org_id"`
		ProjectID string `json:"project_id"`
		Component string `json:"component"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeToolResult(w, id, "invalid arguments", true)
		return
	}
	if a.OrgID == "" || a.ProjectID == "" || a.Component == "" {
		writeToolResult(w, id, "org_id, project_id, and component are required", true)
		return
	}

	creds, err := s.svc.LookupDatabase(ctx, a.OrgID, a.ProjectID, a.Component)
	if err != nil {
		writeToolResult(w, id, fmt.Sprintf("lookup failed: %v", err), true)
		return
	}
	if creds == nil {
		writeToolResult(w, id, fmt.Sprintf(
			"no database found for org=%s project=%s component=%s",
			a.OrgID, a.ProjectID, a.Component,
		), true)
		return
	}

	out, _ := json.MarshalIndent(creds, "", "  ")
	writeToolResult(w, id, string(out), false)
}

// --- JSON-RPC helpers ---

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result}) //nolint:errcheck
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rpcResponse{ //nolint:errcheck
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcErr{Code: code, Message: msg},
	})
}

// writeToolResult formats a tool call result per the MCP content spec.
func writeToolResult(w http.ResponseWriter, id json.RawMessage, text string, isError bool) {
	result := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	}
	if isError {
		result["isError"] = true
	}
	writeResult(w, id, result)
}

func msgSuffix(msg string) string {
	if msg == "" {
		return ""
	}
	return " (" + msg + ")"
}
