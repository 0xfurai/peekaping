package notification_channel

var NotificationChannelProviderRegistry = make(map[string]NotificationChannelProvider)

func RegisterNotificationChannelProvider(name string, provider NotificationChannelProvider) {
	NotificationChannelProviderRegistry[name] = provider
}

func GetNotificationChannelProvider(name string) (NotificationChannelProvider, bool) {
	n, ok := NotificationChannelProviderRegistry[name]
	return n, ok
}
