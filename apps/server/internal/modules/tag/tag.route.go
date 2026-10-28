package tag

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

func (r *Route) ConnectRoute(
	rg *httpx.Router,
	controller *Controller,
) {
	router := rg.Group("tags")

	router.Use(r.middleware.WorkspaceAuth())
	router.Document(httpx.WorkspaceIDHeader)

	httpx.Register(router, "GET", "", huma.Operation{Summary: "Get tags", Tags: []string{"tag"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.FindAll)
	httpx.Register(router, "POST", "", huma.Operation{Summary: "Create tag", Tags: []string{"tag"}, DefaultStatus: http.StatusCreated, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusConflict, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Create)
	httpx.Register(router, "GET", "/{id}", huma.Operation{Summary: "Get tag by ID", Tags: []string{"tag"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, controller.FindByID)
	httpx.Register(router, "PUT", "/{id}", huma.Operation{Summary: "Update tag", Tags: []string{"tag"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusConflict, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.UpdateFull)
	httpx.Register(router, "PATCH", "/{id}", huma.Operation{Summary: "Update tag", Tags: []string{"tag"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusConflict, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.UpdatePartial)
	httpx.Register(router, "DELETE", "/{id}", huma.Operation{Summary: "Delete tag", Tags: []string{"tag"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Delete)
}
