package protocol

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/probe"
	"peekaping/internal/probeproto"
	"peekaping/internal/utils"

	"go.uber.org/zap"
)

// The keys the middleware sets on the request's context. Handlers read them with ProbeFromContext.
const (
	contextProbe    = "probe"
	contextInstance = "probeInstanceId"
)

// The messages of the errors the middleware answers with
const (
	messageTokenRequired   = "a probe token is required: send Authorization: Bearer <probe token>"
	messageInvalidToken    = "invalid probe token"
	messageInstanceHeader  = "the " + probeproto.HeaderInstanceID + " header must carry the instance id once, 1 to 255 visible characters with no spaces"
	messageUnknownInstance = "unknown instance: send hello first"
	messageInternalError   = "Internal server error"
	messageUnavailable     = "the server could not take the request just now: send it again"
)

// maxInstanceIDLength is the most characters an instance id may have, as many as the column that
// stores it
const maxInstanceIDLength = 255

// Auth authenticates the requests of the probe protocol by the probe's token and the instance's
// id. A probe sends the token as Authorization: Bearer <token> and the instance id as
// X-Probe-Instance on every request; the admin's JWT and API keys open none of these routes.
type Auth struct {
	probes probe.Service
	bus    events.EventBus
	logger *zap.SugaredLogger
	now    func() time.Time
}

// NewAuth returns the bearer middleware. The bus carries probe.online, which the middleware
// publishes when a request brings an offline probe back.
func NewAuth(probes probe.Service, bus events.EventBus, logger *zap.SugaredLogger) *Auth {
	return &Auth{probes: probes, bus: bus, logger: logger.Named("[probe-protocol]"), now: time.Now}
}

// Authenticate finds the probe the bearer token belongs to and reads the instance id. It answers
//   - 501 on a MongoDB install, which has no probes
//   - 401 for a missing or malformed Authorization header, an unknown token, and a disabled probe
//   - 400 for a missing, repeated or malformed X-Probe-Instance header
//
// It puts the probe and the instance id on the request's context; ProbeFromContext reads them.
// It does not check that the instance said hello, which only hello does without: KnownInstance
// does, for every other route.
func (a *Auth) Authenticate() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				a.reject(w, r, http.StatusUnauthorized, messageTokenRequired)
				return
			}
			found, err := a.probes.FindByToken(r.Context(), token)
			if err != nil {
				a.fail(w, r, err)
				return
			}
			if found == nil {
				a.reject(w, r, http.StatusUnauthorized, messageInvalidToken)
				return
			}
			instanceID, ok := instanceHeader(r)
			if !ok {
				a.reject(w, r, http.StatusBadRequest, messageInstanceHeader)
				return
			}
			r = httpx.WithValue(r, contextProbe, found)
			r = httpx.WithValue(r, contextInstance, instanceID)
			next.ServeHTTP(w, r)
		})
	}
}

// KnownInstance goes after Authenticate on every route except hello. It stamps the instance's last
// seen time, and answers 409 when the instance has not said hello: the probe has to introduce the
// instance first, as it does when the server was reinstalled or the instance was removed. A probe
// that was offline is online from the stamp on, at once; hello does the same once it has recorded
// the instance.
func (a *Auth) KnownInstance() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, instanceID, ok := ProbeFromContext(r.Context())
			if !ok {
				// KnownInstance was put on a route that Authenticate doesn't guard
				a.logger.Errorw("A probe route checks the instance before authenticating the probe", "route", r.Pattern)
				a.reject(w, r, http.StatusUnauthorized, messageTokenRequired)
				return
			}
			at := a.now().UTC()
			found, err := a.probes.TouchInstance(r.Context(), p.ID, instanceID, at)
			if err != nil {
				a.fail(w, r, err)
				return
			}
			if !found {
				a.reject(w, r, http.StatusConflict, messageUnknownInstance)
				return
			}
			cameBack(r.Context(), a.probes, a.bus, a.logger, p, at)
			next.ServeHTTP(w, r)
		})
	}
}

// ProbeFromContext returns the probe and the instance id that Authenticate put on the context of
// a request. The probe is the one its token belongs to, without its instances or notification
// channels. ok is false for a request that Authenticate did not let through.
func ProbeFromContext(ctx context.Context) (p *probe.Model, instanceID string, ok bool) {
	p, _ = httpx.Value(ctx, contextProbe).(*probe.Model)
	instanceID, _ = httpx.Value(ctx, contextInstance).(string)
	return p, instanceID, p != nil && instanceID != ""
}

// reject answers with the API's usual error body
func (a *Auth) reject(w http.ResponseWriter, r *http.Request, status int, message string) {
	// Never the token or the header: only what went wrong
	httpx.Log(r.Context(), a.logger).Debugw("Probe request rejected", "status", status, "reason", message, "route", r.Pattern)
	httpx.JSON(w, status, utils.NewFailResponse(message))
}

// fail answers a failure of the probe service: 501 on a MongoDB install, 500 for anything else
func (a *Auth) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, probe.ErrUnsupportedStorage) {
		httpx.JSON(w, http.StatusNotImplemented, utils.NewFailResponse(err.Error()))
		return
	}
	httpx.Log(r.Context(), a.logger).Errorw("Probe authentication failed", "route", r.Pattern, "error", err)
	httpx.JSON(w, http.StatusInternalServerError, utils.NewFailResponse(messageInternalError))
}

// bearerToken reads the token of an "Authorization: Bearer <token>" header. Unlike the admin's
// middleware, it requires the scheme: a probe is configured with a token and sends it properly.
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

// instanceHeader reads the instance id of the request
func instanceHeader(r *http.Request) (string, bool) {
	values := r.Header.Values(probeproto.HeaderInstanceID)
	if len(values) != 1 || !validInstanceID(values[0]) {
		return "", false
	}
	return values[0], true
}

// validInstanceID reports whether id is 1 to 255 visible ASCII characters, with no spaces
func validInstanceID(id string) bool {
	if id == "" || len(id) > maxInstanceIDLength {
		return false
	}
	for i := range len(id) {
		if id[i] < '!' || id[i] > '~' {
			return false
		}
	}
	return true
}
