package internal

import (
	"net/http"
	"peekaping/internal/httpx"

	"github.com/0xfurai/oasome"
)

// Retain the two published Swagger entry points while serving the OpenAPI contract.
// Static Swagger UI assets are obsolete; /docs serves the interactive reference.
func swaggerHandler(doc *oasome.Doc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/swagger/index.html":
			http.Redirect(w, r, "/docs", http.StatusTemporaryRedirect)
		case "/swagger/doc.json":
			spec, err := doc.MarshalJSON()
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
