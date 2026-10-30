package workspaceport

import (
	"context"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/limits"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/netguard"

	"go.uber.org/dig"
)

// Service exports and imports a workspace. Every method is for an owner of the workspace the
// context runs in: workspace.ErrNotFound for a context with no workspace and workspace.ErrForbidden
// for any other role, an admin and an API key included. They go through the repositories' and
// services' workspace-scoped methods only.
type Service interface {
	// Export builds the workspace document of the workspace
	Export(ctx context.Context) (*Document, error)
	// PrepareHeartbeatExport checks the caller and lists the workspace's monitors. The returned
	// export then writes the heartbeat file. The check comes first so that a caller who may not
	// export is refused before a byte of the file is written.
	PrepareHeartbeatExport(ctx context.Context) (*HeartbeatExport, error)
	// Import adds the document's records to the workspace in one transaction. A file with anything
	// wrong in itself is refused with an *InvalidFileError, all its problems together, and nothing
	// is written. What the workspace or the install can't take as it is gets changed, or left out,
	// and listed in the result. The monitors are published as created once the transaction has
	// committed, so that none is scheduled before its row exists.
	Import(ctx context.Context, doc *Document) (*ImportResult, error)
}

// Dependencies are the repositories that the export reads, and the services that the import
// writes through. A service's create is the API's own, so that an import holds a record to the same
// rules: they run inside the import's transaction (dbtx).
type Dependencies struct {
	dig.In

	Transactor dbtx.Transactor
	// Limits and Guard are the plan and the private target block: an install without them has no
	// limits and blocks nothing
	Limits limits.Source   `optional:"true"`
	Guard  *netguard.Guard `optional:"true"`

	TagService         tag.Service
	ProxyService       proxy.Service
	ChannelService     notification_channel.Service
	MonitorService     monitor.Service
	MaintenanceService maintenance.Service
	StatusPageService  status_page.Service

	Workspaces           workspace.Repository
	Settings             setting.WorkspaceService
	Tags                 tag.Repository
	Proxies              proxy.Repository
	Channels             notification_channel.Repository
	Providers            *notification_channel.ProviderRegistry
	Monitors             monitor.MonitorRepository
	MonitorNotifications monitor_notification.Repository
	MonitorTags          monitor_tag.Repository
	Maintenances         maintenance.Repository
	MonitorMaintenances  monitor_maintenance.Repository
	StatusPages          status_page.Repository
	Domains              domain_status_page.Repository
	MonitorStatusPages   monitor_status_page.Repository
	Heartbeats           heartbeat.Repository
	Probes               probe.Repository
}

type ServiceImpl struct {
	deps Dependencies
	now  func() time.Time
}

func NewService(deps Dependencies) Service {
	return &ServiceImpl{deps: deps, now: time.Now}
}

// owner returns the workspace the request runs in, for a caller who owns it
func owner(ctx context.Context) (workspace.ID, error) {
	if err := workspace.RequireRole(ctx, workspace.RoleOwner); err != nil {
		return "", err
	}
	id, _, _ := workspace.FromContext(ctx)
	return id, nil
}
