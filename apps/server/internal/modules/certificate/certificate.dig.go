package certificate

import (
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/shared"
	"peekaping/internal/utils"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) error {
	return utils.Provide(container, func(
		settingService shared.WorkspaceSettingService,
		monitors monitor.Service,
		eventBus events.EventBus,
		notificationHistoryService notification_sent_history.Service,
		tlsInfoService monitor_tls_info.Service,
		logger *zap.SugaredLogger,
	) Service {
		// Use event-based notification service to integrate with existing notification system
		notificationService := NewEventBasedNotificationService(eventBus, logger)
		return NewService(settingService, monitors, notificationService, notificationHistoryService, tlsInfoService, logger)
	})
}
