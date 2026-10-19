package badge

import (
	"peekaping/internal/httpx"
)

type Route struct {
	controller *Controller
}

func NewRoute(controller *Controller) *Route {
	return &Route{
		controller: controller,
	}
}

func (r *Route) ConnectRoute(rg *httpx.Router, controller *Controller) {
	// Badge routes - these are public endpoints
	badge := rg.Group("badge")
	{
		// Status badge
		badge.Handle("GET", "/{monitorId}/status", r.controller.GetStatusBadge)

		// Uptime badge with duration
		badge.Handle("GET", "/{monitorId}/uptime/{duration}", r.controller.GetUptimeBadge)
		badge.Handle("GET", "/{monitorId}/uptime", r.controller.GetUptimeBadge) // Default duration

		// Ping badge with duration
		badge.Handle("GET", "/{monitorId}/ping/{duration}", r.controller.GetPingBadge)
		badge.Handle("GET", "/{monitorId}/ping", r.controller.GetPingBadge) // Default duration

		// Certificate expiry badge
		badge.Handle("GET", "/{monitorId}/cert-exp", r.controller.GetCertExpBadge)

		// Response time badge
		badge.Handle("GET", "/{monitorId}/response", r.controller.GetResponseBadge)
	}
}
