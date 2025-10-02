package healthcheck

import (
	"net/http"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/utils"
	"strconv"
	"time"

	"peekaping/internal/modules/shared"

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

func RegisterPushEndpoint(
	router *gin.RouterGroup,
	monitorService monitor.Service,
	heartbeatService heartbeat.Service,
	healthcheckSupervisor *HealthCheckSupervisor,
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

		// Parse status and ping
		statusInt, err := strconv.Atoi(statusStr)
		if err != nil {
			statusInt = 1
		}
		status := shared.MonitorStatus(statusInt)

		result := &executor.Result{
			Status:    status,
			Message:   msg,
			StartTime: time.Now().UTC(),
			EndTime:   time.Now().UTC(),
		}

		healthcheckSupervisor.postProcessHeartbeat(result, monitor, nil)

		ctx.JSON(http.StatusOK, gin.H{"ok": "true"})
	})
}
