package notification_channel

import (
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/middleware"
	"peekaping/internal/utils"
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

	router.Use(uc.middleware.WorkspaceAuth())
	router.DocumentWorkspace()

	httpx.Register(router, "GET", "", httpx.Operation{Summary: "Get notification channels", Tags: []string{"notification_channel"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.FindAll)
	httpx.Register(router, "POST", "", httpx.Operation{Responses: map[string]*httpx.Response{"400": httpx.ErrorVariants[utils.TargetNotAllowedResponse]()}, Summary: "Create notification channel", Tags: []string{"notification_channel"}, DefaultStatus: http.StatusCreated, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Create)
	httpx.Register(router, "POST", "/test", httpx.Operation{Responses: map[string]*httpx.Response{"400": httpx.ErrorVariants[utils.TargetNotAllowedResponse]()}, Summary: "Test notification channel", Tags: []string{"notification_channel"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Test)
	httpx.Register(router, "GET", "/{id}", httpx.Operation{Summary: "Get notification channel by ID", Tags: []string{"notification_channel"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, controller.FindByID)
	httpx.Register(router, "PUT", "/{id}", httpx.Operation{Responses: map[string]*httpx.Response{"400": httpx.ErrorVariants[utils.TargetNotAllowedResponse]()}, Summary: "Update notification channel", Tags: []string{"notification_channel"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.UpdateFull)
	httpx.Register(router, "PATCH", "/{id}", httpx.Operation{Responses: map[string]*httpx.Response{"400": httpx.ErrorVariants[utils.TargetNotAllowedResponse]()}, Summary: "Update notification channel", Tags: []string{"notification_channel"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.UpdatePartial)
	httpx.Register(router, "DELETE", "/{id}", httpx.Operation{Summary: "Delete notification channel", Tags: []string{"notification_channel"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.Delete)
}
