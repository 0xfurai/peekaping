package probe

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

func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	probes := router.Group("probes")

	// Every member, and an API key, sees the probes (docs/adr/0005 section 10): an instance's host
	// and the probe's channels only if they are an instance admin. Changing a probe is the instance
	// admin's alone, since a probe belongs to the install and not to a workspace: another user gets
	// 403, and so does an API key, which belongs to a workspace.
	read, manage := r.middleware.AllAuth(), r.middleware.InstanceAdminAuth()
	{
		httpx.Register(probes, "POST", "", huma.Operation{Summary: "Create probe", Description: "Only an instance admin may do this: any other user and any API key get 403.", Tags: []string{"probe"}, DefaultStatus: http.StatusCreated, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized, http.StatusForbidden}}, controller.CreateProbe, manage)
		httpx.Register(probes, "GET", "", huma.Operation{Summary: "Get probes", Description: "Every probe, the local probe first. A user who is not an instance admin, and an API key, see each probe with its name, location, status and supported monitor types, but its instances come with an empty hostname, OS, architecture and version and a protocol version of 0, and notification_ids is an empty list.", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusUnauthorized}}, controller.GetProbes, read)
		// availability is a literal segment, which the mux prefers to the {id} of the next route
		httpx.Register(probes, "GET", "availability", huma.Operation{Summary: "Get the availability of every probe", Description: "Every probe's online and offline periods over the range, and its uptime, without its instances.", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.GetProbesAvailability, read)
		httpx.Register(probes, "GET", "{id}/availability", huma.Operation{Summary: "Get the availability of a probe", Description: "A probe's online and offline periods over the range and its uptime, with its instances and when each was connected over the last 24 hours. A user who is not an instance admin, and an API key, see each instance with an empty hostname and version.", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError, http.StatusUnauthorized}}, controller.GetProbeAvailability, read)
		httpx.Register(probes, "GET", "{id}", huma.Operation{Summary: "Get probe", Description: "A user who is not an instance admin, and an API key, see the probe's name, location, status and supported monitor types, but its instances come with an empty hostname, OS, architecture and version and a protocol version of 0, and notification_ids is an empty list.", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.SharedAuth, Errors: []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusUnauthorized}}, controller.GetProbe, read)
		httpx.Register(probes, "PUT", "{id}", huma.Operation{Summary: "Update probe", Description: "Only an instance admin may do this: any other user and any API key get 403.", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError, http.StatusUnauthorized, http.StatusForbidden}}, controller.UpdateProbe, manage)
		httpx.Register(probes, "DELETE", "{id}", huma.Operation{Summary: "Delete probe", Description: "Only an instance admin may do this: any other user and any API key get 403.", Tags: []string{"probe"}, DefaultStatus: http.StatusNoContent, Security: httpx.JWTOnly, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError, http.StatusUnauthorized, http.StatusForbidden}}, controller.DeleteProbe, manage)
		httpx.Register(probes, "POST", "{id}/assign-all-monitors", huma.Operation{Summary: "Assign all monitors to a probe", Description: "Only an instance admin may do this: any other user and any API key get 403. Adds the probe to every monitor it can run now: push monitors are left out, and so are the types the probe's online instances don't run. A probe with no instance yet is assigned every monitor by its first hello instead, and the answer says it is pending.", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError, http.StatusUnauthorized, http.StatusForbidden}}, controller.AssignAllMonitors, manage)
		httpx.Register(probes, "POST", "{id}/rotate-token", huma.Operation{Summary: "Rotate probe token", Description: "Only an instance admin may do this: any other user and any API key get 403.", Tags: []string{"probe"}, DefaultStatus: http.StatusOK, Security: httpx.JWTOnly, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError, http.StatusUnauthorized, http.StatusForbidden}}, controller.RotateProbeToken, manage)
	}
}
