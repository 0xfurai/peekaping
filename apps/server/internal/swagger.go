package internal

import (
	"encoding/json"
	"net/http"
	"peekaping/internal/httpx"

	"github.com/danielgtaylor/huma/v2"
)

// Retain the two published Swagger entry points while serving the Huma contract.
// Static Swagger UI assets are obsolete; /docs serves the interactive reference.
func swaggerHandler(api huma.API) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/swagger/index.html":
			http.Redirect(w, r, "/docs", http.StatusTemporaryRedirect)
		case "/swagger/doc.json":
			spec, err := json.Marshal(api.OpenAPI())
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write(spec)
		default:
			httpx.Text(w, http.StatusNotFound, "Not Found")
		}
	}
}
