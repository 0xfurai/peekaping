package badge

import (
	"net/http"
	"peekaping/internal/httpx"

	"github.com/danielgtaylor/huma/v2"
)

type Route struct {
	controller *Controller
}

func NewRoute(controller *Controller) *Route {
	return &Route{
		controller: controller,
	}
}

// restrictBrowser lets a browser show a badge but run nothing from it, so a
// script that reached its SVG still wouldn't run.
func restrictBrowser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, req)
	})
}

func (r *Route) ConnectRoute(rg *httpx.Router, controller *Controller) {
	// Badge routes - these are public endpoints
	badge := rg.Group("badge")
	badge.Use(restrictBrowser)
	{
		// Status badge
		httpx.Register(badge, "GET", "/{monitorId}/status", huma.Operation{Summary: "Get status badge", Tags: []string{"badge"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetStatusBadge)

		// Uptime badge with duration
		httpx.Register(badge, "GET", "/{monitorId}/uptime/{duration}", huma.Operation{Summary: "Get uptime badge", Tags: []string{"badge"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetUptimeBadge)
		httpx.Register(badge, "GET", "/{monitorId}/uptime", huma.Operation{Summary: "Get uptime badge", Tags: []string{"badge"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetUptimeBadge) // Default duration

		// Ping badge with duration
		httpx.Register(badge, "GET", "/{monitorId}/ping/{duration}", huma.Operation{Summary: "Get ping badge", Tags: []string{"badge"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetPingBadge)
		httpx.Register(badge, "GET", "/{monitorId}/ping", huma.Operation{Summary: "Get ping badge", Tags: []string{"badge"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetPingBadge) // Default duration

		// Certificate expiry badge
		httpx.Register(badge, "GET", "/{monitorId}/cert-exp", huma.Operation{Summary: "Get certificate expiry badge", Tags: []string{"badge"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetCertExpBadge)

		// Response time badge
		httpx.Register(badge, "GET", "/{monitorId}/response", huma.Operation{Summary: "Get response time badge", Tags: []string{"badge"}, DefaultStatus: http.StatusOK, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusNotFound}}, r.controller.GetResponseBadge)
	}
}
