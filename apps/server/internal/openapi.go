package internal

import (
	"peekaping/internal/config"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/badge"
	"peekaping/internal/modules/bruteforce"
	"peekaping/internal/modules/error_reporting"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe/protocol"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/telemetry"

	"github.com/danielgtaylor/huma/v2"
	"go.uber.org/zap"
)

// OpenAPI builds the same server registrations without starting infrastructure.
// Dependencies are only used when a request reaches a handler, never to register
// its typed contract. This also makes missing export operations testable against
// the production server, rather than keeping a separate documentation route list.
func OpenAPI() *huma.OpenAPI {
	server := ProvideServer(
		zap.NewNop().Sugar(), &config.Config{}, nil, nil,
		monitor.NewMonitorRoute(nil, nil), nil,
		auth.NewRoute(nil, nil, nil, &bruteforce.AccountGuard{}), nil, nil,
		notification_channel.NewRoute(nil, nil), nil,
		proxy.NewRoute(nil, nil), nil,
		setting.NewRoute(nil, nil), nil,
		nil, nil, nil,
		maintenance.NewRoute(nil, nil), nil, nil,
		status_page.NewRoute(nil, nil), nil,
		tag.NewRoute(nil, nil), nil,
		telemetry.NewRoute(nil, nil, nil), nil,
		error_reporting.NewRoute(nil, nil, nil), nil,
		badge.NewRoute(nil), nil,
		api_key.NewRoute(nil, nil), nil,
		probe.NewRoute(nil, nil), nil,
		protocol.NewRoute(nil, nil), nil,
		nil, nil,
	)
	return server.Router.API().OpenAPI()
}
