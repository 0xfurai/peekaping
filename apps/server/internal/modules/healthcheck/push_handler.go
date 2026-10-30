package healthcheck

import (
	"context"
	"math"
	"net/http"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/utils"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"peekaping/internal/modules/shared"

	"go.uber.org/zap"
)

type PushConfig struct {
	PushToken string `json:"pushToken"`
}

// RegisterPushEndpoint registers the push URL. Each push goes to the ingester service, which
// stores it in order with the monitor's other results (ingest.NewInOrderService).
func RegisterPushEndpoint(
	router *httpx.Router,
	monitorService monitor.Service,
	heartbeatService heartbeat.Service,
	ingesterService ingester.Service,
	maintenanceService maintenance.Service,
	logger *zap.SugaredLogger,
) {
	// The call across workspaces is made here, where this function is listed for it in
	// internal/isolation_test.go
	findByToken := func(ctx context.Context, token string) (*monitor.Model, error) {
		return monitorService.FindOneByPushTokenAnyWorkspace(ctx, token)
	}
	handler := &pushHandler{findByToken: findByToken, ingesterService: ingesterService, maintenanceService: maintenanceService, logger: logger}
	httpx.Register(router, "GET", "/push/{token}", huma.Operation{Summary: "Send a heartbeat to a push monitor", Description: "Stores the push as the monitor's heartbeat. 404 when no monitor has the token, 400 when the monitor is paused, and 403 when the monitor's workspace is suspended (a new account's workspace is, until its owner verifies their email address, and an instance admin can suspend one), when its type is turned off on this install (DISABLED_MONITOR_TYPES), or when it is past its workspace's limit of active monitors: the push is not stored and sends no notification.", Tags: []string{"Push"}, Errors: []int{400, 403, 404, 500}}, handler.handle)
}

// pushHandler answers the push URL
type pushHandler struct {
	findByToken        func(ctx context.Context, token string) (*monitor.Model, error)
	ingesterService    ingester.Service
	maintenanceService maintenance.Service
	logger             *zap.SugaredLogger
}

func (h *pushHandler) handle(ctx context.Context, input *PushInput) (*httpx.Output[PushResponse], error) {
	r := input.Request
	token := r.PathValue("token")

	monitor, err := h.findByToken(ctx, token)
	if err != nil {
		h.logger.Errorw("Failed to find monitor with push token", "error", err)
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Monitor not found for pushToken"))
	}
	if monitor == nil {
		h.logger.Errorw("Monitor not found for push token")
		return nil, httpx.Fail(http.StatusNotFound, utils.NewFailResponse("Monitor not found for pushToken"))
	}
	if refusal := h.refusal(monitor); refusal != nil {
		return nil, refusal
	}

	status := parsePushStatus(r.URL.Query().Get("status"))
	msg := r.URL.Query().Get("msg")
	if msg == "" {
		msg = defaultPushMessage(status)
	}
	ping := 0

	// A push monitor inside an active maintenance window reports
	// Maintenance, like a scheduled check, so planned downtime neither
	// shows as Down nor sends notifications.
	underMaintenance, err := MonitorUnderMaintenance(ctx, h.maintenanceService, monitor.ID)
	if err != nil {
		// Keep the heartbeat the service pushed rather than lose it.
		h.logger.Errorw("Failed to check if push monitor is under maintenance",
			"monitor_id", monitor.ID,
			"error", err,
		)
	}
	if !underMaintenance {
		ping = h.pushedPing(r.URL.Query().Get("ping"), monitor.ID)
	}

	now := time.Now().UTC()

	// The ingester service stores the push before the request is answered, in order with the
	// monitor's other results
	result := ResultFromMonitor(monitor, underMaintenance, status, msg, ping, now, now, nil, h.logger)

	err = h.ingesterService.Ingest(ctx, result)
	if err != nil {
		h.logger.Errorw("Failed to ingest push heartbeat",
			"monitor_id", monitor.ID,
			"error", err,
		)
		return nil, httpx.Fail(http.StatusInternalServerError, utils.NewFailResponse("Failed to process push heartbeat"))
	}

	return httpx.Reply(http.StatusOK, PushResponse{OK: "true"})
}

// refusal is the error that answers a push to a monitor that takes no pushes now, or nil
func (h *pushHandler) refusal(monitor *monitor.Model) error {
	if monitor.WorkspaceSuspended {
		// The token holder is told why their pushes are not counted, as for a paused monitor: the
		// token is the secret that names the monitor. Nothing is stored and nothing is sent.
		h.logger.Infow("Push refused: the monitor's workspace is suspended", "monitor_id", monitor.ID)
		return httpx.Fail(http.StatusForbidden, utils.NewFailResponse(suspendedMessage(monitor)))
	}
	if monitor.TypeDisabled {
		// Told the same way: the token holder should know why their pushes are not counted
		h.logger.Infow("Push refused: the monitor's type is turned off on this install", "monitor_id", monitor.ID)
		return httpx.Fail(http.StatusForbidden, utils.NewFailResponse("This monitor's type is turned off on this install"))
	}
	if monitor.OverLimit {
		// And when the monitor is past its workspace's limit of active monitors (PP-160)
		h.logger.Infow("Push refused: the monitor is past its workspace's limit of active monitors", "monitor_id", monitor.ID)
		return httpx.Fail(http.StatusForbidden, utils.NewFailResponse("This monitor is past its workspace's limit of active monitors"))
	}
	if !monitor.Active {
		// Only the ID: the monitor's config holds credentials
		h.logger.Errorw("Monitor is not active", "monitor_id", monitor.ID)
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("Monitor is not active"))
	}
	return nil
}

// suspendedMessage tells the token holder which hold keeps their monitor's workspace from running:
// an owner's scheduled deletion, an instance admin's suspension, or the email verification
func suspendedMessage(monitor *monitor.Model) string {
	switch {
	case monitor.WorkspaceDeleting:
		return "The workspace of this monitor is scheduled for deletion"
	case monitor.WorkspaceSuspendedByAdmin:
		return "The workspace of this monitor is suspended by the install's administrator"
	default:
		return "The workspace of this monitor is suspended until its owner verifies their email address"
	}
}

// pushedPing is the ping the push reports, which is 0 when it is invalid
func (h *pushHandler) pushedPing(pingStr, monitorID string) int {
	ping, ok := parsePushPing(pingStr)
	if !ok {
		h.logger.Warnw("Ignoring invalid push ping", "monitor_id", monitorID, "ping", pingStr)
	}
	return ping
}

// maintenanceMessage matches the message a scheduled check records while the
// monitor is inside a maintenance window.
const maintenanceMessage = "Monitor under maintenance"

// MonitorUnderMaintenance reports whether any maintenance window attached to
// the monitor is active. A window that can't be read doesn't hide another
// active window; the caller keeps the reported heartbeat rather than drop it.
func MonitorUnderMaintenance(ctx context.Context, service maintenance.Service, monitorID string) (bool, error) {
	maintenances, err := service.GetMaintenancesByMonitorIDAnyWorkspace(ctx, monitorID)
	if err != nil {
		return false, err
	}
	var firstErr error
	for _, m := range maintenances {
		underMaintenance, err := service.IsUnderMaintenance(ctx, m)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if underMaintenance {
			return true, nil
		}
	}
	return false, firstErr
}

// parsePushStatus maps the status query value to a monitor status. Like
// Uptime Kuma, whose push URL format this follows, an empty value means up
// and anything other than "up" or "1" means down, so a failure reported with
// an unexpected word is never recorded as healthy.
func parsePushStatus(value string) shared.MonitorStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "up", "1":
		return shared.MonitorStatusUp
	default:
		return shared.MonitorStatusDown
	}
}

func defaultPushMessage(status shared.MonitorStatus) string {
	if status == shared.MonitorStatusUp {
		return "OK"
	}
	return "Down"
}

// maxPushPingMs keeps the ping inside the INTEGER heartbeats.ping column.
const maxPushPingMs = math.MaxInt32

// parsePushPing returns the ping in whole milliseconds, rounding decimals.
// An empty value is 0. A malformed, negative or oversized value is dropped as
// 0 and reported with ok=false, so a bad ping never costs the heartbeat.
func parsePushPing(value string) (ping int, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, true
	}
	ms, err := strconv.ParseFloat(value, 64)
	// The range check is written so that NaN fails it too.
	if err != nil || !(ms >= 0 && ms <= maxPushPingMs) {
		return 0, false
	}
	return int(math.Round(ms)), true
}

type PushInput = httpx.Input[httpx.NoBody, PushParams]
type PushParams struct {
	Token  string `path:"token" doc:"Secret push token, authenticating the monitor."`
	Status string `query:"status" doc:"Empty, up, or 1 means Up; other values mean Down."`
	Msg    string `query:"msg" doc:"Heartbeat message; defaults to OK or Down."`
	Ping   string `query:"ping" doc:"Response time in milliseconds; malformed values are ignored."`
}
type PushResponse struct {
	OK string `json:"ok"`
}
