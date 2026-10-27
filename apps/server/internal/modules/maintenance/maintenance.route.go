package maintenance

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/middleware"

	"github.com/danielgtaylor/huma/v2"
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
	httpx.Register(router, "GET", "", huma.Operation{Summary: "Get maintenances", Tags: []string{"maintenance"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.FindAll)
	httpx.Register(router, "POST", "", huma.Operation{Summary: "Create maintenance", Tags: []string{"maintenance"}, DefaultStatus: http.StatusCreated, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.Create)
	httpx.Register(router, "GET", "{id}", huma.Operation{Summary: "Get maintenance by ID", Tags: []string{"maintenance"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, uc.controller.FindByID)
	httpx.Register(router, "PUT", "{id}", huma.Operation{Summary: "Update maintenance", Tags: []string{"maintenance"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.UpdateFull)
	httpx.Register(router, "PATCH", "{id}", huma.Operation{Summary: "Update maintenance", Tags: []string{"maintenance"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.UpdatePartial)
	httpx.Register(router, "DELETE", "{id}", huma.Operation{Summary: "Delete maintenance", Tags: []string{"maintenance"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.Delete)

	httpx.Register(router, "PATCH", "{id}/pause", huma.Operation{Summary: "Pause maintenance", Tags: []string{"maintenance"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.Pause)
	httpx.Register(router, "PATCH", "{id}/resume", huma.Operation{Summary: "Resume maintenance", Tags: []string{"maintenance"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, uc.controller.Resume)
}
