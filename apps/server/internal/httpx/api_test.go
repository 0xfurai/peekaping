package httpx_test

import (
	"context"
	"encoding/json"
	"peekaping/internal/httpx"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nullableLimits struct {
	Monitors int `json:"monitors"`
}

type nullableRequest struct {
	Limits *nullableLimits `json:"limits,omitempty"`
	Rule   *string         `json:"rule,omitempty" enum:"any,all"`
}

type nullableResponse struct {
	Reason *string `json:"reason" nullable:"true" enum:"admin,unverified"`
}

type nullableParams struct{}

// A nullable field documents null as one of its values: in the enum of a nullable enum, and
// beside the $ref of a nullable object.
func TestOpenAPI_NullableFieldsAcceptNull(t *testing.T) {
	router := httpx.NewRouter()
	handler := func(context.Context, *httpx.Input[*nullableRequest, nullableParams]) (*httpx.Output[nullableResponse], error) {
		return httpx.Reply(200, nullableResponse{})
	}
	// Two operations share the schemas, so the generator documents them twice.
	httpx.Register(router, "POST", "/things", huma.Operation{}, handler)
	httpx.Register(router, "PUT", "/things", huma.Operation{}, handler)
	spec, err := json.Marshal(router.API().OpenAPI())
	require.NoError(t, err)
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(spec)))
	require.NoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, compiler.AddResource("https://peekaping.test/openapi.json", doc))
	compile := func(name string) *jsonschema.Schema {
		t.Helper()
		schema, err := compiler.Compile("https://peekaping.test/openapi.json#/components/schemas/" + name)
		require.NoError(t, err)
		return schema
	}
	request, response := compile("httpx_test.nullableRequest"), compile("httpx_test.nullableResponse")
	for _, tc := range []struct {
		schema *jsonschema.Schema
		body   string
		valid  bool
	}{
		{request, `{"limits":null}`, true},
		{request, `{"limits":{"monitors":3}}`, true},
		{request, `{"limits":"none"}`, false},
		{request, `{"rule":null}`, true},
		{request, `{"rule":"all"}`, true},
		{request, `{"rule":"some"}`, false},
		{response, `{"reason":null}`, true},
		{response, `{"reason":"admin"}`, true},
		{response, `{"reason":"other"}`, false},
	} {
		value, err := jsonschema.UnmarshalJSON(strings.NewReader(tc.body))
		require.NoError(t, err)
		if tc.valid {
			assert.NoError(t, tc.schema.Validate(value), tc.body)
		} else {
			assert.Error(t, tc.schema.Validate(value), tc.body)
		}
	}
}
