package workspaceport

import (
	"net/http"
	"reflect"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace/workspacehttp"

	"github.com/danielgtaylor/huma/v2"
)

type Route struct {
	controller *Controller
	middleware workspacehttp.RouteAuth
}

func NewRoute(controller *Controller, middleware workspacehttp.RouteAuth) *Route {
	return &Route{controller: controller, middleware: middleware}
}

func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	// A download is a user's own act, like a deletion: an API key has no user, so it can't
	export := router.Group("workspace/export")
	export.Use(r.middleware.WorkspaceJWTAuth())
	export.DocumentWorkspace()

	disposition := map[string]*huma.Param{"Content-Disposition": {Description: "attachment, with the file's name", Schema: &huma.Schema{Type: "string"}}}
	httpx.Register(export, "GET", "", huma.Operation{
		Summary:     "Download the workspace's setup",
		Description: "An owner of the workspace downloads one JSON file (peekaping-workspace-<date>.json) with its settings, tags, proxies, notification channels, monitors, maintenance windows and status pages, and the links between them. Records point to each other by ref, not by database ID. The file holds saved secrets in full (containsSecrets is always true), and each channel and proxy lists which of its fields are sensitive: keep the file as safe as the secrets are. Monitor and channel configs are JSON objects. It holds no members, invites, API keys, stats, probe assignments or heartbeats. Only a signed-in owner may download it: an admin or a member is refused (403), and so is an API key (401), since a download is a user's act.",
		Tags:        []string{"workspace"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly,
		Responses: map[string]*huma.Response{"200": {Description: "The workspace document", Headers: disposition, Content: map[string]*huma.MediaType{
			"application/json": {Schema: export.API().OpenAPI().Components.Schemas.Schema(reflect.TypeFor[Document](), true, "Document")},
		}}},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError},
	}, controller.Export)

	httpx.Register(export, "GET", "heartbeats", huma.Operation{
		Summary:     "Download the workspace's heartbeat history",
		Description: "An owner of the workspace downloads the heartbeats of its monitors as NDJSON (peekaping-heartbeats-<date>.ndjson): the first line is a header (format peekaping-heartbeats, version, exportedAt), then one line for each heartbeat, ordered by monitor (as in the workspace document) and then by time. The server streams the file in batches, so a large history is never held in memory. A monitorRef is the ref the monitor has in the workspace document. If the server fails part way, the connection is cut, so a file that ends early is not complete. The same rules as the workspace document apply: owner only, 403 for an admin or a member, 401 for an API key.",
		Tags:        []string{"workspace"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly,
		Responses: map[string]*huma.Response{"200": {Description: "The heartbeat file", Headers: disposition, Content: map[string]*huma.MediaType{
			"application/x-ndjson": {Schema: &huma.Schema{Type: "string", Description: "One JSON value per line: a HeartbeatHeader, then HeartbeatLine values"}},
		}}},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError},
	}, controller.ExportHeartbeats)
}
