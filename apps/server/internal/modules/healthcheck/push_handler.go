package healthcheck

import (
	"fmt"
	"net/http"
	"vigi/internal/modules/heartbeat"
	"vigi/internal/modules/monitor"
	"vigi/internal/modules/queue"
	"vigi/internal/utils"
	"strconv"
	"time"

	"vigi/internal/modules/shared"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type PushHeartbeatRequest struct {
	PushToken string `json:"pushToken" binding:"required"`
	Status    int    `json:"status" binding:"required"`
	Msg       string `json:"msg"`
	Ping      int    `json:"ping"`
}

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

		// pingStr := ctx.DefaultQuery("ping", "0")

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

		msg := ctx.DefaultQuery("msg", "OK")
		statusStr := ctx.DefaultQuery("status", "1")

		// Parse status
		statusInt, err := strconv.Atoi(statusStr)
		if err != nil {
			statusInt = 1
		}
		status := shared.MonitorStatus(statusInt)

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
			PingMs:             0, // Push monitors don't have meaningful ping times
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
