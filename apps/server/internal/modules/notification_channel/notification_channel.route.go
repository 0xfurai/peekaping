package notification_channel

import (
	"peekaping/internal/httpx"
	"peekaping/internal/modules/middleware"
)

type Route struct {
	controller *Controller
	middleware *middleware.AuthChain
}

func NewRoute(
	controller *Controller,
	middleware *middleware.AuthChain,
) *Route {
	return &Route{
		controller,
		middleware,
	}
}

func (uc *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("notification-channels")

	router.Use(uc.middleware.AllAuth())

	router.Handle("GET", "", controller.FindAll)
	router.Handle("POST", "", controller.Create)
	router.Handle("POST", "/test", controller.Test)
	router.Handle("GET", "/{id}", controller.FindByID)
	router.Handle("PUT", "/{id}", controller.UpdateFull)
	router.Handle("PATCH", "/{id}", controller.UpdatePartial)
	router.Handle("DELETE", "/{id}", controller.Delete)
}
