package tag

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

func (r *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("tags")

	router.Use(r.middleware.AllAuth())

	router.Handle("GET", "", controller.FindAll)
	router.Handle("POST", "", controller.Create)
	router.Handle("GET", "/{id}", controller.FindByID)
	router.Handle("PUT", "/{id}", controller.UpdateFull)
	router.Handle("PATCH", "/{id}", controller.UpdatePartial)
	router.Handle("DELETE", "/{id}", controller.Delete)
}
