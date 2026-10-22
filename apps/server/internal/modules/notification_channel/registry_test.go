package notification_channel

// newTestRegistry returns a registry of the given providers. It keeps the map, so a test can
// add providers to the map after.
func newTestRegistry(byType map[string]NotificationChannelProvider) *ProviderRegistry {
	return &ProviderRegistry{byType: byType}
}
