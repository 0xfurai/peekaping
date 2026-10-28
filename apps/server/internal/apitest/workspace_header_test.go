package apitest

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type specParameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Schema      struct {
		Type   string `json:"type"`
		Format string `json:"format"`
	} `json:"schema"`
}

// workspaceHeaderParams are the X-Workspace-ID parameters an operation lists
func workspaceHeaderParams(parameters []specParameter) []specParameter {
	var found []specParameter
	for _, p := range parameters {
		if p.In == "header" && p.Name == "X-Workspace-ID" {
			found = append(found, p)
		}
	}
	return found
}

// publicPaths are paths in a workspace route group that are public or belong to no workspace
var publicPaths = map[string]bool{
	"/api/v1/api-keys/config":                            true,
	"/api/v1/status-pages/slug/{slug}":                   true,
	"/api/v1/status-pages/domain/{domain}":               true,
	"/api/v1/status-pages/slug/{slug}/monitors":          true,
	"/api/v1/status-pages/slug/{slug}/monitors/homepage": true,
}

func TestAPIReference_EveryWorkspaceOperationDocumentsTheWorkspaceHeader(t *testing.T) {
	a := newApp(t)
	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []specParameter `json:"parameters"`
		} `json:"paths"`
	}
	w := a.request(t, http.MethodGet, "/openapi.json", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &spec))

	workspacePrefixes := []string{"/api/v1/monitors", "/api/v1/notification-channels", "/api/v1/status-pages", "/api/v1/proxies", "/api/v1/maintenances", "/api/v1/tags", "/api/v1/settings", "/api/v1/api-keys"}
	checked := 0
	for path, operations := range spec.Paths {
		inWorkspace := false
		for _, prefix := range workspacePrefixes {
			inWorkspace = inWorkspace || path == prefix || strings.HasPrefix(path, prefix+"/")
		}
		for method, op := range operations {
			found := workspaceHeaderParams(op.Parameters)
			if !inWorkspace || publicPaths[path] {
				assert.Empty(t, found, "%s %s is no workspace route", method, path)
				continue
			}
			checked++
			if assert.Len(t, found, 1, "%s %s", method, path) {
				header := found[0]
				assert.False(t, header.Required, "%s %s", method, path)
				assert.Equal(t, "string", header.Schema.Type)
				assert.Equal(t, "uuid", header.Schema.Format)
				assert.Equal(t, "The workspace the request runs in. Without it, the user's default workspace, the one they joined first. An API key always runs in its own workspace.", header.Description)
			}
		}
	}
	assert.Greater(t, checked, 30, "the workspace routes were found")
	assert.Empty(t, workspaceHeaderParams(spec.Paths["/api/v1/auth/login"]["post"].Parameters), "login names no workspace")
	assert.Empty(t, workspaceHeaderParams(spec.Paths["/api/v1/me"]["get"].Parameters), "/me names no workspace")
}
