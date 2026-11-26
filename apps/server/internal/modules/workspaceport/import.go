package workspaceport

import (
	"context"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/status_page"
)

// ImportPath is the route of an import, under /api/v1
const ImportPath = "workspace/import"

// MaxImportBody is the most an import request may send, 16 MiB: more than the API's 1 MiB for any
// other route, since a workspace's whole setup is one file
const MaxImportBody = 16 << 20

// importPlan is a document fitted to the workspace and the install: the records that will be
// written, and what was changed or left out on the way. A record that is left out is in no list,
// and nothing in the file is linked to it.
type importPlan struct {
	settings settingsPlan
	// reusedTags maps the ref of a file's tag to the ID of the tag of that name that the workspace has
	reusedTags   map[string]string
	tags         []Tag
	proxies      []Proxy
	channels     []NotificationChannel
	monitors     []plannedMonitor
	maintenances []Maintenance
	pages        []plannedPage
	changes      []Change
}

// settingsPlan holds the settings to store: 0 and nil leave a setting as it is
type settingsPlan struct {
	retentionDays int
	certDays      []int
}

// plannedMonitor is a monitor as the service will create it. Its links are refs of the file, which
// the write turns into the IDs of the records it made.
type plannedMonitor struct {
	ref         string
	dto         *monitor.CreateUpdateDto
	proxyRef    string
	channelRefs []string
	tagRefs     []string
}

type plannedPage struct {
	ref         string
	dto         *status_page.CreateStatusPageDTO
	monitorRefs []string
}

func (p *importPlan) add(change Change) { p.changes = append(p.changes, change) }

func (s *ServiceImpl) Import(ctx context.Context, doc *Document) (*ImportResult, error) {
	id, err := owner(ctx)
	if err != nil {
		return nil, err
	}
	var found problems
	s.checkFile(doc, &found)
	plan, err := s.fit(ctx, id, doc, &found)
	if err != nil {
		return nil, err
	}
	if len(found) > 0 {
		return nil, &InvalidFileError{Problems: found}
	}
	result, created, err := s.write(ctx, id, plan)
	if err != nil {
		return nil, err
	}
	// The monitors are in the database now, so a listener that loads one finds it and its links
	s.deps.MonitorService.PublishCreated(ctx, created)
	return result, nil
}
