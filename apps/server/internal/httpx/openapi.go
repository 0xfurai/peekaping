package httpx

import (
	"bytes"
	"net/http"
	"peekaping/internal/version"
	"reflect"
	"strconv"
	"strings"

	"github.com/0xfurai/oasome"
	"gopkg.in/yaml.v3"
)

// operationDoc is the documentation of one operation
type operationDoc struct {
	options   []oasome.RouteOption
	responses map[int]*Response
	// body is the type of the request body
	body reflect.Type
}

// responseKey names a response of an operation in the document
type responseKey struct {
	path, method string
	status       int
}

// OpenAPI is the router's OpenAPI document, of every operation registered with Register
func (r *Router) OpenAPI() *oasome.Doc { return r.registry.openAPI() }

// openAPI is the document, which it makes on first use. The document serves itself at
// /openapi.json and /openapi.yaml, with the reference at /docs.
func (reg *registry) openAPI() *oasome.Doc {
	if reg.doc != nil {
		return reg.doc
	}
	reg.requestTypes = map[reflect.Type]bool{}
	reg.descriptions = map[responseKey]string{}
	reg.doc = oasome.New("Peekaping API", version.Version,
		oasome.Server("/", ""),
		oasome.SecurityScheme("JwtAuth", oasome.Scheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"}),
		oasome.SecurityScheme("ApiKeyAuth", oasome.Scheme{Type: "apiKey", In: "header", Name: "X-API-Key"}),
		oasome.SecurityScheme("ProbeAuth", oasome.Scheme{Type: "http", Scheme: "bearer", BearerFormat: "probe token"}),
		oasome.Naming(componentName),
		oasome.FieldSchema(reg.fieldSchema),
		oasome.Extend(reg.extend),
	)
	root := &Router{registry: reg}
	root.Handle(http.MethodGet, "/openapi.json", reg.serveJSON)
	root.Handle(http.MethodGet, "/openapi.yaml", reg.serveYAML)
	root.Handle(http.MethodGet, "/docs", serveReference)
	return reg.doc
}

// document adds an operation to the document
func (reg *registry) document(method, pattern string, op operationDoc) {
	doc := reg.openAPI()
	if body := op.bodyType(); body != nil {
		reg.requestTypes[body] = true
	}
	for status, response := range op.responses {
		if response.Description != "" {
			reg.descriptions[responseKey{pattern, strings.ToLower(method), status}] = response.Description
		}
	}
	options := append(op.options, oasome.OperationID(operationID(method, pattern)))
	doc.RouteHandler(method+" "+pattern, http.NotFoundHandler(), options...)
}

// bodyType is the named struct type of the request body, nil for an operation without one. A
// struct without a name has no schema of its own, so it keeps the plain rules.
func (op operationDoc) bodyType() reflect.Type {
	if op.body == nil {
		return nil
	}
	t := derefType(op.body)
	if t.Kind() != reflect.Struct || t.Name() == "" || t == reflect.TypeFor[NoBody]() {
		return nil
	}
	return t
}

// componentNames turns a Go type name into the name of its schema
var componentNames = strings.NewReplacer("shared.Monitor", "monitor.Model", "shared.Proxy", "proxy.Model", "shared.HeartBeatModel", "heartbeat.Model",
	"interface{}", "Any", "[]", "Array_", "[", "_", "]", "", "*", "", "/", "_", ",", "_", " ", "")

// componentName names the schema of a struct type by its package and name, as tag.Model, and a
// generic one with its type arguments, as utils.ApiResponse_tag.Model
func componentName(t reflect.Type) string {
	name := strings.ReplaceAll(strings.ReplaceAll(t.String(), "peekaping/internal/modules/", ""), " ", "")
	return componentNames.Replace(name)
}

// extend sets what oasome has no option for: whether objects accept unknown properties, the
// response descriptions of the operations that have their own, parameter examples only in the
// parameter's schema, request bodies that are not null, and that an operation without security of
// its own is public
func (reg *registry) extend(spec map[string]any) {
	spec["security"] = []any{}
	schemas, _ := dig(spec, "components", "schemas").(map[string]any)
	for t := range reg.requestTypes {
		if schema, ok := schemas[componentName(t)].(map[string]any); ok {
			// A request accepts unknown fields, as the decoder does
			schema["additionalProperties"] = true
		}
	}
	closeObjects(spec)
	for key, description := range reg.descriptions {
		if response, ok := dig(spec, "paths", key.path, key.method, "responses", strconv.Itoa(key.status)).(map[string]any); ok {
			response["description"] = description
		}
	}
	paths, _ := spec["paths"].(map[string]any)
	for _, item := range paths {
		dropParameterExamples(item)
		requireBodies(item)
	}
}

// requireBodies documents the request body of each of a path's operations as its value alone: a
// handler that decodes into a pointer refuses null all the same
func requireBodies(item any) {
	operations, _ := item.(map[string]any)
	for _, operation := range operations {
		content, _ := dig(operation, "requestBody", "content").(map[string]any)
		for _, media := range content {
			media, _ := media.(map[string]any)
			if value := nonNull(media["schema"]); value != nil {
				media["schema"] = value
			}
		}
	}
}

// nonNull is the schema that a schema of anyOf it and null allows besides null, nil for another
// schema
func nonNull(schema any) any {
	options, _ := dig(schema, "anyOf").([]any)
	if len(options) != 2 || dig(options[1], "type") != "null" {
		return nil
	}
	return options[0]
}

// dig is the value at keys in nested objects, nil when one is missing
func dig(value any, keys ...string) any {
	for _, key := range keys {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

// closeObjects documents every object schema in value that says nothing of unknown properties as
// refusing them, as the encoder never sends any
func closeObjects(value any) {
	switch v := value.(type) {
	case map[string]any:
		if _, set := v["additionalProperties"]; !set && isObject(v["type"]) {
			v["additionalProperties"] = false
		}
		for _, child := range v {
			closeObjects(child)
		}
	case []any:
		for _, child := range v {
			closeObjects(child)
		}
	}
}

// isObject is whether a schema's type is object, alone or with null
func isObject(schemaType any) bool {
	switch t := schemaType.(type) {
	case string:
		return t == "object"
	case []any:
		for _, each := range t {
			if each == "object" {
				return true
			}
		}
	}
	return false
}

// dropParameterExamples removes the example of each parameter of a path's operations: the
// parameter's schema has it, in examples
func dropParameterExamples(item any) {
	operations, _ := item.(map[string]any)
	for _, operation := range operations {
		parameters, _ := dig(operation, "parameters").([]any)
		for _, parameter := range parameters {
			if p, ok := parameter.(map[string]any); ok {
				delete(p, "example")
			}
		}
	}
}

func (reg *registry) serveJSON(w http.ResponseWriter, _ *http.Request) {
	spec, err := reg.doc.MarshalJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json")
	_, _ = w.Write(spec)
}

func (reg *registry) serveYAML(w http.ResponseWriter, _ *http.Request) {
	spec, err := SpecYAML(reg.doc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+yaml")
	_, _ = w.Write(spec)
}

// SpecYAML is the document as YAML, with the keys in the order of its JSON
func SpecYAML(doc *oasome.Doc) ([]byte, error) {
	spec, err := doc.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(spec, &node); err != nil {
		return nil, err
	}
	blockStyle(&node)
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&node); err != nil {
		return nil, err
	}
	err = encoder.Close()
	return out.Bytes(), err
}

// blockStyle drops the JSON flow style of node and of the nodes in it, so that the YAML is in
// block style, and quotes only the strings that need it
func blockStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		blockStyle(child)
	}
}

// serveReference is the interactive API reference, which reads /openapi.yaml
func serveReference(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="referrer" content="same-origin" />
    <meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no" />
    <title>Peekaping API Reference</title>
    <link href="https://unpkg.com/@stoplight/elements@9.0.0/styles.min.css" rel="stylesheet" />
    <script src="https://unpkg.com/@stoplight/elements@9.0.0/web-components.min.js" integrity="sha256-Tqvw1qE2abI+G6dPQBc5zbeHqfVwGoamETU3/TSpUw4="
            crossorigin="anonymous"></script>
  </head>
  <body style="height: 100vh;">
    <elements-api
      apiDescriptionUrl="/openapi.yaml"
      router="hash"
      layout="sidebar"
      tryItCredentialsPolicy="same-origin"
    />
  </body>
</html>`))
}

func operationID(method, path string) string {
	id := strings.ToLower(method)
	if probePath, ok := strings.CutPrefix(path, "/probe/v1"); ok {
		// The probe protocol's ids start with probe, so that they can't collide with the API's
		// (probePostHello, probeGetAssignments)
		path = probePath
		id = "probe" + strings.ToUpper(id[:1]) + id[1:]
	}
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1"), "/")
	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "{") {
			id += "By"
			part = strings.Trim(part, "{}")
		}
		for _, word := range strings.Split(part, "-") {
			id += strings.ToUpper(word[:1]) + word[1:]
		}
	}
	if path == "/health" {
		id += "Root"
	}
	return id
}
