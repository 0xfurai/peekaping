package certificate

import (
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/shared"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

func RegisterDependencies(container *dig.Container) {
	container.Provide(func(
		settingService shared.SettingService,
		eventBus *events.EventBus,
		notificationHistoryService notification_sent_history.Service,
		tlsInfoService monitor_tls_info.Service,
		logger *zap.SugaredLogger,
	) Service {
		// Use event-based notification service to integrate with existing notification system
		notificationService := NewEventBasedNotificationService(eventBus, logger)
		return NewService(settingService, notificationService, notificationHistoryService, tlsInfoService, logger)
	})

}
