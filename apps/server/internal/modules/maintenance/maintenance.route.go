package maintenance

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
	router := rg.Group("maintenances")

	router.Use(uc.middleware.AllAuth())
	router.Handle("GET", "", uc.controller.FindAll)
	router.Handle("POST", "", uc.controller.Create)
	router.Handle("GET", "{id}", uc.controller.FindByID)
	router.Handle("PUT", "{id}", uc.controller.UpdateFull)
	router.Handle("PATCH", "{id}", uc.controller.UpdatePartial)
	router.Handle("DELETE", "{id}", uc.controller.Delete)

	router.Handle("PATCH", "{id}/pause", uc.controller.Pause)
	router.Handle("PATCH", "{id}/resume", uc.controller.Resume)
}
