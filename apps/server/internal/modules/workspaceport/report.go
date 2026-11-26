package workspaceport

import (
	"fmt"
	"strings"
)

// What an import answers: how many records it made, and every change it made to the file's
// records to fit the workspace and the install (PP-170). A change is never an error.

// ImportResult is the answer to an import
type ImportResult struct {
	Created Created  `json:"created"`
	Changes []Change `json:"changes"`
}

// Created counts the records an import made. A tag the workspace had already is reused, not created.
type Created struct {
	Tags                 int `json:"tags"`
	Proxies              int `json:"proxies"`
	NotificationChannels int `json:"notificationChannels"`
	Monitors             int `json:"monitors"`
	Maintenances         int `json:"maintenances"`
	StatusPages          int `json:"statusPages"`
}

// The kinds of change, and the records they are about
const (
	ChangeTagReused                      = "tag_reused"
	ChangeProxySkippedTargetNotAllowed   = "proxy_skipped_target_not_allowed"
	ChangeMonitorProxyDropped            = "monitor_proxy_dropped"
	ChangeChannelSkipped                 = "channel_skipped"
	ChangeMonitorSkippedTypeDisabled     = "monitor_skipped_type_disabled"
	ChangeMonitorSkippedTargetNotAllowed = "monitor_skipped_target_not_allowed"
	ChangeMonitorIntervalRaised          = "monitor_interval_raised"
	ChangePushTokenReplaced              = "push_token_replaced"
	ChangeMonitorPausedOverLimit         = "monitor_paused_over_limit"
	ChangeStatusPageSlugChanged          = "status_page_slug_changed"
	ChangeStatusPageSkippedOverLimit     = "status_page_skipped_over_limit"
	ChangeCustomDomainDropped            = "custom_domain_dropped"
	ChangeCustomDomainDroppedOverLimit   = "custom_domain_dropped_over_limit"
	ChangeRetentionCapped                = "retention_capped"

	RecordTag                 = "tag"
	RecordProxy               = "proxy"
	RecordNotificationChannel = "notificationChannel"
	RecordMonitor             = "monitor"
	RecordMaintenance         = "maintenance"
	RecordStatusPage          = "statusPage"
	RecordCustomDomain        = "customDomain"
	RecordSettings            = "settings"
	// RecordFile is a problem with the file as a whole
	RecordFile = "file"
)

// Change is one thing an import did differently from what the file says
type Change struct {
	Kind   string `json:"kind" doc:"What changed, for example tag_reused or monitor_paused_over_limit" example:"monitor_interval_raised"`
	Record string `json:"record" enum:"tag,proxy,notificationChannel,monitor,maintenance,statusPage,customDomain,settings" doc:"The kind of record the change is about. A custom domain's ref is its status page's."`
	Ref    string `json:"ref,omitempty" doc:"The record's ref in the file"`
	Name   string `json:"name,omitempty" doc:"The record's name, title, slug, domain or address, to tell it by"`
	From   string `json:"from,omitempty" doc:"The value in the file, when a value changed"`
	To     string `json:"to,omitempty" doc:"The value the import stored, when a value changed"`
	Reason string `json:"reason,omitempty" doc:"Why, when the kind doesn't say: the code of the refusal of a skipped channel, or the field whose value was raised (interval or retryInterval)"`
}

// Problem is something wrong with the file. An import that finds any writes nothing.
type Problem struct {
	Record  string `json:"record" doc:"The kind of record, or file for the file as a whole" example:"monitor"`
	Ref     string `json:"ref,omitempty" doc:"The record's ref in the file"`
	Message string `json:"message" example:"config is invalid: url is required"`
}

// InvalidFileError is the problems that keep a file from being imported, all of them together
type InvalidFileError struct {
	Problems []Problem
}

func (e *InvalidFileError) Error() string {
	messages := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		messages[i] = p.Record + " " + p.Ref + ": " + p.Message
	}
	return fmt.Sprintf("the file can't be imported: %s", strings.Join(messages, "; "))
}

// problems collects what is wrong with a file
type problems []Problem

func (p *problems) add(record, ref, format string, args ...any) {
	*p = append(*p, Problem{Record: record, Ref: ref, Message: fmt.Sprintf(format, args...)})
}
