package api

import (
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/controllers"
)

// registerWebhookRoutes mounts the inbound GitHub webhook receiver. The routes
// live outside the JWT middleware (same pattern the now-removed /mcp/ mount
// used) — webhooks authenticate via HMAC, not JWT. Both patterns are more
// specific than the JWT-gated "/api/" subtree, so net/http's ServeMux routes
// them here rather than into the auth middleware.
//
// Two paths are registered for the same handler:
//   - /webhooks/github         — local/dev (smee delivers here).
//   - /api/v1/webhooks/github  — cloud. The gateway's webhook endpoint is
//     scoped to base path /api/v1/webhooks and forwards to the BFF verbatim,
//     so GitHub deliveries arrive here. (jwtAuth is disabled on that gateway
//     endpoint; the per-repo webhook URL is GITHUB_WEBHOOK_DELIVERY_URL.)
func registerWebhookRoutes(mux *http.ServeMux, c controllers.WebhookController) {
	mux.HandleFunc("POST /webhooks/github", c.Receive)
	mux.HandleFunc("POST /api/v1/webhooks/github", c.Receive)
}
