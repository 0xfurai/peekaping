package healthcheck

import (
	"fmt"
	"math"
	"net/http"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/queue"
	"peekaping/internal/utils"
	"strconv"
	"strings"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type PushConfig struct {
	PushToken string `json:"pushToken"`
}

// IngesterTaskPayload matches the payload structure for ingester tasks
type PushIngesterPayload struct {
	MonitorID          string               `json:"monitor_id"`
	MonitorName        string               `json:"monitor_name"`
	MonitorType        string               `json:"monitor_type"`
	MonitorInterval    int                  `json:"monitor_interval"`
	MonitorTimeout     int                  `json:"monitor_timeout"`
	MonitorMaxRetries  int                  `json:"monitor_max_retries"`
	MonitorRetryInt    int                  `json:"monitor_retry_interval"`
	MonitorResendInt   int                  `json:"monitor_resend_interval"`
	MonitorConfig      string               `json:"monitor_config"`
	Status             shared.MonitorStatus `json:"status"`
	Message            string               `json:"message"`
	PingMs             int                  `json:"ping_ms"`
	StartTime          time.Time            `json:"start_time"`
	EndTime            time.Time            `json:"end_time"`
	IsUnderMaintenance bool                 `json:"is_under_maintenance"`
	TLSInfo            interface{}          `json:"tls_info,omitempty"`
	CheckCertExpiry    bool                 `json:"check_cert_expiry"`
}

func RegisterPushEndpoint(
	router *gin.RouterGroup,
	monitorService monitor.Service,
	heartbeatService heartbeat.Service,
	queueService queue.Service,
	logger *zap.SugaredLogger,
) {
	router.GET("/push/:token", func(ctx *gin.Context) {
		token := ctx.Param("token")

		monitor, err := monitorService.FindOneByPushToken(ctx, token)
		if err != nil {
			logger.Errorw("Failed to find monitor with push token", "error", err)
			ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found for pushToken"))
			return
		}
		if monitor == nil {
			logger.Errorw("Monitor not found for push token", "pushToken", token)
			ctx.JSON(http.StatusNotFound, utils.NewFailResponse("Monitor not found for pushToken"))
			return
		}
		if !monitor.Active {
			logger.Errorw("Monitor is not active", "monitor", monitor)
			ctx.JSON(http.StatusBadRequest, utils.NewFailResponse("Monitor is not active"))
			return
		}

		status := parsePushStatus(ctx.Query("status"))
		msg := ctx.Query("msg")
		if msg == "" {
			msg = defaultPushMessage(status)
		}
		pingStr := ctx.Query("ping")
		ping, ok := parsePushPing(pingStr)
		if !ok {
			logger.Warnw("Ignoring invalid push ping", "monitor_id", monitor.ID, "ping", pingStr)
		}

		now := time.Now().UTC()

		// Enqueue to ingester instead of processing directly
		payload := PushIngesterPayload{
			MonitorID:          monitor.ID,
			MonitorName:        monitor.Name,
			MonitorType:        monitor.Type,
			MonitorInterval:    monitor.Interval,
			MonitorTimeout:     monitor.Timeout,
			MonitorMaxRetries:  monitor.MaxRetries,
			MonitorRetryInt:    monitor.RetryInterval,
			MonitorResendInt:   monitor.ResendInterval,
			MonitorConfig:      monitor.Config,
			Status:             status,
			Message:            msg,
			PingMs:             ping,
			StartTime:          now,
			EndTime:            now,
			IsUnderMaintenance: false, // Push monitors don't have maintenance windows in the same way
			TLSInfo:            nil,
			CheckCertExpiry:    false,
		}

		opts := &queue.EnqueueOptions{
			Queue:     "ingester",
			MaxRetry:  3,
			Timeout:   2 * time.Minute,
			Retention: 1 * time.Hour,
		}

		// Use EnqueueUnique to prevent duplicate push heartbeat ingestion
		// The unique key includes monitor ID and timestamp to prevent duplicate submissions
		uniqueKey := fmt.Sprintf("ingest:push:%s:%d", monitor.ID, now.UnixNano())
		ttl := 5 * time.Minute // Short TTL for push monitors to allow frequent updates

		_, err = queueService.EnqueueUnique(ctx, "monitor:ingest", payload, uniqueKey, ttl, opts)
		if err != nil {
			logger.Errorw("Failed to enqueue push heartbeat to ingester",
				"monitor_id", monitor.ID,
				"error", err,
			)
			ctx.JSON(http.StatusInternalServerError, utils.NewFailResponse("Failed to process push heartbeat"))
			return
		}

		ctx.JSON(http.StatusOK, gin.H{"ok": "true"})
	})
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
