package setting

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
		controller, middleware,
	}
}

func (uc *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("/settings")

	router.Use(uc.middleware.AllAuth())

	router.Handle("GET", "key/{key}", uc.controller.GetByKey)
	router.Handle("PUT", "key/{key}", uc.controller.SetByKey)
	router.Handle("DELETE", "key/{key}", uc.controller.DeleteByKey)
}
