package httpx_test

import (
	"context"
	"encoding/json"
	"peekaping/internal/httpx"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rulesNested struct {
	Label string `json:"label" validate:"required"`
}

// rulesRequest has a field for each rule the documentation reads from a validate tag
type rulesRequest struct {
	Name     string       `json:"name" validate:"required,min=2,max=10"`
	Note     string       `json:"note,omitempty" validate:"omitempty,min=1,max=5"`
	Email    string       `json:"email" validate:"omitempty,email"`
	Color    string       `json:"color" validate:"omitempty,hexcolor"`
	Kind     string       `json:"kind" validate:"oneof=a b"`
	Mode     *string      `json:"mode" validate:"omitempty,oneof=x y"`
	Count    int          `json:"count" validate:"min=1,max=9"`
	Ratio    float64      `json:"ratio" validate:"min=0.5"`
	Zero     int          `json:"zero" validate:"min=0"`
	Tags     []string     `json:"tags" validate:"required,min=1,max=3,dive,min=2,max=4"`
	Ids      []*string    `json:"ids,omitempty" validate:"dive,oneof=p q"`
	Numbers  []int        `json:"numbers" validate:"required"`
	Broken   string       `json:"broken" validate:"min=abc"`
	Pointer  *int         `json:"pointer"`
	Strict   *int         `json:"strict" nullable:"false"`
	Required *string      `json:"required" validate:"required"`
	Nested   *rulesNested `json:"nested,omitempty"`
	Unnamed  string       `json:",omitempty"`
	Hidden   string       `json:"-"`
}

type rulesParams struct{}

func specOf(t *testing.T, router *httpx.Router) map[string]any {
	t.Helper()
	raw, err := json.Marshal(router.OpenAPI())
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, json.Unmarshal(raw, &spec))
	return spec
}

func TestOpenAPI_RequestRulesAreDocumentedFromValidateTags(t *testing.T) {
	router := httpx.NewRouter()
	httpx.Register(router, "POST", "/rules", httpx.Operation{},
		func(context.Context, *httpx.Input[rulesRequest, rulesParams]) (*httpx.Output[struct{}], error) {
			return httpx.Reply(200, struct{}{})
		})

	spec := specOf(t, router)

	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	raw, err := json.Marshal(schemas["httpx_test.rulesRequest"])
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"type": "object",
		"additionalProperties": true,
		"required": ["count", "name", "numbers", "ratio", "required", "tags"],
		"properties": {
			"Unnamed": {"type": "string"},
			"broken": {"type": "string"},
			"color": {"type": "string", "pattern": "^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$"},
			"count": {"type": "integer", "format": "int64", "minimum": 1, "maximum": 9},
			"email": {"type": "string", "format": "email"},
			"ids": {"type": ["array", "null"], "items": {"type": ["string", "null"], "enum": ["p", "q", null]}},
			"kind": {"type": "string", "enum": ["a", "b"]},
			"mode": {"type": ["string", "null"], "enum": ["x", "y", null]},
			"name": {"type": "string", "minLength": 2, "maxLength": 10},
			"nested": {"anyOf": [{"$ref": "#/components/schemas/httpx_test.rulesNested"}, {"type": "null"}]},
			"note": {"type": "string", "minLength": 1, "maxLength": 5},
			"numbers": {"type": "array", "items": {"type": "integer", "format": "int64"}},
			"pointer": {"type": ["integer", "null"], "format": "int64"},
			"ratio": {"type": "number", "format": "double", "minimum": 0.5},
			"required": {"type": ["string", "null"]},
			"strict": {"type": "integer", "format": "int64"},
			"tags": {"type": "array", "minItems": 1, "maxItems": 3, "items": {"type": "string", "minLength": 2, "maxLength": 4}},
			"zero": {"type": "integer", "format": "int64", "minimum": 0}
		}
	}`, string(raw))
}

// A body that is not a named object has no rules to read
func TestOpenAPI_ABodyThatIsNotANamedObjectKeepsItsPlainSchema(t *testing.T) {
	router := httpx.NewRouter()
	httpx.Register(router, "PUT", "/names", httpx.Operation{},
		func(context.Context, *httpx.Input[[]string, rulesParams]) (*httpx.Output[struct{}], error) {
			return httpx.Reply(200, struct{}{})
		})
	httpx.Register(router, "PUT", "/labels", httpx.Operation{},
		func(context.Context, *httpx.Input[map[string]string, rulesParams]) (*httpx.Output[struct{}], error) {
			return httpx.Reply(200, struct{}{})
		})

	httpx.Register(router, "PUT", "/inline", httpx.Operation{},
		func(context.Context, *httpx.Input[struct {
			Name string `json:"name" validate:"required"`
		}, rulesParams]) (*httpx.Output[struct{}], error) {
			return httpx.Reply(200, struct{}{})
		})

	paths := specOf(t, router)["paths"].(map[string]any)

	for path, want := range map[string]string{
		"/names":  `{"type": "array", "items": {"type": "string"}}`,
		"/labels": `{"type": "object", "additionalProperties": {"type": "string"}}`,
		"/inline": `{"type": "object", "additionalProperties": false, "properties": {"name": {"type": "string"}}, "required": ["name"]}`,
	} {
		put := paths[path].(map[string]any)["put"].(map[string]any)
		body, err := json.Marshal(put["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"])
		require.NoError(t, err)
		// A struct without a name has no schema of its own to loosen
		assert.JSONEq(t, want, string(body), path)
	}
}

type opParams struct {
	ID    string `path:"id" doc:"The thing"`
	Page  int    `query:"page" doc:"Page number"`
	Token string `query:"token" required:"true"`
	Plain string
}

type opBody struct {
	Name string `json:"name"`
}

func TestOpenAPI_OperationsDocumentTheirParametersAndResponses(t *testing.T) {
	router := httpx.NewRouter()
	group := router.Group("/api/v1")
	group.DocumentWorkspace()
	httpx.Register(group, "GET", "/things/{id}", httpx.Operation{Errors: []int{404}},
		func(context.Context, *httpx.Input[httpx.NoBody, opParams]) (*httpx.Output[opBody], error) {
			return httpx.Reply(200, opBody{})
		})
	httpx.Register(group, "DELETE", "/things/{id}", httpx.Operation{DefaultStatus: 204, Responses: map[string]*httpx.Response{
		"304": {Description: "Not modified: nothing changed"},
	}},
		func(context.Context, *httpx.Input[opBody, opParams]) (*httpx.Output[httpx.NoBody], error) {
			return httpx.Reply(204, httpx.NoBody{})
		})
	httpx.Register(group, "GET", "/badge", httpx.Operation{},
		func(context.Context, *httpx.Input[httpx.NoBody, rulesParams]) (*httpx.Output[httpx.SVGBody], error) {
			return httpx.SVG("<svg/>")
		})

	spec := specOf(t, router)

	raw, err := json.Marshal(spec["paths"])
	require.NoError(t, err)
	workspace := `{"name": "X-Workspace-ID", "in": "header", "schema": {"type": "string", "format": "uuid"},
		"description": "The workspace the request runs in. Without it, the user's default workspace, the one they joined first. An API key always runs in its own workspace."}`
	// oasome lists the parameters by location: path, header, query
	thingPath := `{"name": "id", "in": "path", "required": true, "description": "The thing", "schema": {"type": "string", "description": "The thing"}}`
	thingQuery := `{"name": "page", "in": "query", "description": "Page number", "schema": {"type": "integer", "format": "int64", "description": "Page number"}},
		{"name": "token", "in": "query", "required": true, "schema": {"type": "string"}}`
	tooMany := `{"description": "Too Many Requests",
		"content": {"application/json": {"schema": {"$ref": "#/components/schemas/httpx.ErrorBody"}}},
		"headers": {"Retry-After": {"description": "Seconds to wait before sending the request again.", "schema": {"type": "integer"}}}}`
	assert.JSONEq(t, `{
		"/api/v1/badge": {"get": {
			"operationId": "getBadge",
			"parameters": [`+workspace+`],
			"responses": {
				"200": {"description": "OK", "content": {"image/svg+xml": {"schema": {"type": "string"}}},
					"headers": {"Cache-Control": {"schema": {"type": "string"}}}},
				"429": `+tooMany+`
			}
		}},
		"/api/v1/things/{id}": {
			"get": {
				"operationId": "getThingsById",
				"parameters": [`+thingPath+`, `+workspace+`, `+thingQuery+`],
				"responses": {
					"200": {"description": "OK", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/httpx_test.opBody"}}}},
					"404": {"description": "Not Found", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/httpx.ErrorBody"}}}},
					"429": `+tooMany+`
				}
			},
			"delete": {
				"operationId": "deleteThingsById",
				"parameters": [`+thingPath+`, `+workspace+`, `+thingQuery+`],
				"requestBody": {"required": true, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/httpx_test.opBody"}}}},
				"responses": {
					"204": {"description": "No Content"},
					"304": {"description": "Not modified: nothing changed"},
					"429": `+tooMany+`
				}
			}
		}
	}`, string(raw))
}
