package badge

import (
	"github.com/gin-gonic/gin"
)

type Route struct {
	controller *Controller
}

func NewRoute(controller *Controller) *Route {
	return &Route{
		controller: controller,
	}
}

func (r *Route) ConnectRoute(rg *gin.RouterGroup, controller *Controller) {
	// Badge routes - these are public endpoints
	badge := rg.Group("badge")
	{
		// Status badge
		badge.GET("/:monitorId/status", r.controller.GetStatusBadge)

		// Uptime badge with duration
		badge.GET("/:monitorId/uptime/:duration", r.controller.GetUptimeBadge)
		badge.GET("/:monitorId/uptime", r.controller.GetUptimeBadge) // Default duration

		// Ping badge with duration
		badge.GET("/:monitorId/ping/:duration", r.controller.GetPingBadge)
		badge.GET("/:monitorId/ping", r.controller.GetPingBadge) // Default duration

		// Average response badge with duration
		badge.GET("/:monitorId/avg-response/:duration", r.controller.GetAvgResponseBadge)
		badge.GET("/:monitorId/avg-response", r.controller.GetAvgResponseBadge) // Default duration

		// Certificate expiry badge
		badge.GET("/:monitorId/cert-exp", r.controller.GetCertExpBadge)

		// Response time badge
		badge.GET("/:monitorId/response", r.controller.GetResponseBadge)
	}
}
