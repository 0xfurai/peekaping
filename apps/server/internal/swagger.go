package internal

import (
	"net/http"
	"peekaping/internal/httpx"
	"strings"

	swaggerFiles "github.com/swaggo/files"
	"github.com/swaggo/swag"
)

func swaggerHandler() http.HandlerFunc {
	assets := http.StripPrefix("/swagger", http.FileServer(swaggerFiles.HTTP))
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/swagger/index.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(swaggerIndex))
		case "/swagger/doc.json":
			spec, err := swag.ReadDoc()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(spec))
		default:
			name := strings.TrimPrefix(r.URL.Path, "/swagger/")
			if strings.HasSuffix(name, ".js") {
				w.Header().Set("Content-Type", "application/javascript")
			}
			switch name {
			case "swagger-ui.css", "swagger-ui.css.map", "swagger-ui.js", "swagger-ui.js.map", "swagger-ui-bundle.js", "swagger-ui-bundle.js.map", "swagger-ui-standalone-preset.js", "swagger-ui-standalone-preset.js.map", "favicon-16x16.png", "favicon-32x32.png", "oauth2-redirect.html":
				assets.ServeHTTP(w, r)
			default:
				httpx.Text(w, http.StatusNotFound, "Not Found")
			}
		}
	}
}

const swaggerIndex = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Swagger UI</title>
<link rel="stylesheet" href="./swagger-ui.css"></head><body>
<div id="swagger-ui"></div><script src="./swagger-ui-bundle.js"></script>
<script src="./swagger-ui-standalone-preset.js"></script><script>
window.onload=function(){window.ui=SwaggerUIBundle({url:"/swagger/doc.json",dom_id:"#swagger-ui",deepLinking:true,presets:[SwaggerUIBundle.presets.apis,SwaggerUIStandalonePreset],plugins:[SwaggerUIBundle.plugins.DownloadUrl],layout:"StandaloneLayout",persistAuthorization:false});};
</script></body></html>`
