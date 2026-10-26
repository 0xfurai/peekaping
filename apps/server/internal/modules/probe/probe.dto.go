package probe

import "time"

// CreateProbeDto represents the request to create a probe
type CreateProbeDto struct {
	Name     string `json:"name" validate:"required,min=1,max=255"`
	Location string `json:"location,omitempty" validate:"omitempty,max=255"`
	// NotificationIDs are the notification channels that hear about the probe. A probe with none
	// notifies the channels of its active monitors instead.
	NotificationIDs []string `json:"notification_ids,omitempty" validate:"omitempty"`
}

// UpdateProbeDto represents the request to update a probe. A field left out stays as it is.
type UpdateProbeDto struct {
	Name     *string `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	Location *string `json:"location,omitempty" validate:"omitempty,max=255"`
	Disabled *bool   `json:"disabled,omitempty" validate:"omitempty"`
	// NotificationIDs replaces the probe's notification channels when present. An empty list
	// clears them.
	NotificationIDs []string `json:"notification_ids,omitempty" validate:"omitempty"`
}

// ProbeInstanceResponse is one process that connected with a probe's token
type ProbeInstanceResponse struct {
	ID              string `json:"id" validate:"required"`
	Hostname        string `json:"hostname" validate:"required"`
	OS              string `json:"os" validate:"required"`
	Arch            string `json:"arch" validate:"required"`
	Version         string `json:"version" validate:"required"`
	ProtocolVersion int    `json:"protocol_version" validate:"required"`
	// SupportedTypes are the monitor types this instance's build runs: never null, and empty for
	// an instance that runs every type
	SupportedTypes []string  `json:"supported_types" validate:"required" nullable:"false"`
	LastSeenAt     time.Time `json:"last_seen_at" validate:"required"`
}

// ProbeResponse represents the response for probe operations. It never holds the token or its
// hash: TokenPrefix, the token's first 8 characters, is all it shows.
type ProbeResponse struct {
	ID          string `json:"id" validate:"required"`
	Name        string `json:"name" validate:"required"`
	Location    string `json:"location" validate:"required"`
	TokenPrefix string `json:"token_prefix" validate:"required"`
	Disabled    bool   `json:"disabled" validate:"required"`
	// Status is online while one of the probe's instances is connected
	Status string `json:"status" validate:"required" enum:"online,offline"`
	// The lists are never null: a probe with none has an empty list
	SupportedTypes  []string                 `json:"supported_types" validate:"required" nullable:"false"`
	NotificationIDs []string                 `json:"notification_ids" validate:"required" nullable:"false"`
	Instances       []*ProbeInstanceResponse `json:"instances" validate:"required" nullable:"false"`
	CreatedAt       time.Time                `json:"created_at" validate:"required"`
	UpdatedAt       time.Time                `json:"updated_at" validate:"required"`
}

// ProbeWithTokenResponse represents the response when creating a probe or rotating its token. It
// is the only response that holds the token, and the only time the API shows it.
type ProbeWithTokenResponse struct {
	ProbeResponse
	Token string `json:"token" validate:"required"`
}

// ToProbeResponse converts a Model to ProbeResponse
func ToProbeResponse(m *Model) *ProbeResponse {
	supportedTypes := m.SupportedTypes
	if supportedTypes == nil {
		supportedTypes = []string{}
	}
	notificationIDs := m.NotificationIDs
	if notificationIDs == nil {
		notificationIDs = []string{}
	}
	instances := make([]*ProbeInstanceResponse, len(m.Instances))
	for i, instance := range m.Instances {
		instanceTypes := instance.SupportedTypes
		if instanceTypes == nil {
			instanceTypes = []string{}
		}
		instances[i] = &ProbeInstanceResponse{
			ID:              instance.ID,
			Hostname:        instance.Hostname,
			OS:              instance.OS,
			Arch:            instance.Arch,
			Version:         instance.Version,
			ProtocolVersion: instance.ProtocolVersion,
			SupportedTypes:  instanceTypes,
			LastSeenAt:      instance.LastSeenAt,
		}
	}
	return &ProbeResponse{
		ID:              m.ID,
		Name:            m.Name,
		Location:        m.Location,
		TokenPrefix:     m.TokenPrefix,
		Disabled:        m.Disabled,
		Status:          string(m.Status),
		SupportedTypes:  supportedTypes,
		NotificationIDs: notificationIDs,
		Instances:       instances,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

// ToProbeWithTokenResponse converts a ModelWithToken to ProbeWithTokenResponse
func ToProbeWithTokenResponse(m *ModelWithToken) *ProbeWithTokenResponse {
	return &ProbeWithTokenResponse{ProbeResponse: *ToProbeResponse(&m.Model), Token: m.Token}
}

// ProbeAvailabilityPeriod is a stretch of time that a probe was online or offline
type ProbeAvailabilityPeriod struct {
	Status string    `json:"status" validate:"required" enum:"online,offline"`
	Start  time.Time `json:"start" validate:"required"`
	End    time.Time `json:"end" validate:"required"`
}

// ProbeAvailabilityConnection is a stretch of time that a probe instance was connected
type ProbeAvailabilityConnection struct {
	Start time.Time `json:"start" validate:"required"`
	// End is when the connection ended, or now for an open one
	End time.Time `json:"end" validate:"required"`
	// Open is true while the instance is connected
	Open bool `json:"open" validate:"required"`
}

// ProbeAvailabilityInstance is a probe instance with when it was connected over the last 24 hours
type ProbeAvailabilityInstance struct {
	ID          string                         `json:"id" validate:"required"`
	Hostname    string                         `json:"hostname" validate:"required"`
	Version     string                         `json:"version" validate:"required"`
	Connections []*ProbeAvailabilityConnection `json:"connections" validate:"required" nullable:"false"`
}

// ProbeAvailabilityResponse is when a probe was online over a range
type ProbeAvailabilityResponse struct {
	ProbeID string `json:"probe_id" validate:"required"`
	Range   string `json:"range" validate:"required" enum:"24h,7d,30d"`
	// From is the start of the range and To its end, now
	From time.Time `json:"from" validate:"required"`
	To   time.Time `json:"to" validate:"required"`
	// InProcess is true for the local probe in single mode, where Peekaping itself checks its
	// monitors: it has no periods, uptime or instances then
	InProcess bool `json:"in_process" validate:"required"`
	// TrackedFrom is the later of From and the probe's first recorded status: the uptime counts
	// from there. Null when the probe has no history.
	TrackedFrom *time.Time `json:"tracked_from" nullable:"true"`
	// UptimePercent is the time online over the time tracked, from 0 to 100. Null when nothing
	// is tracked.
	UptimePercent *float64 `json:"uptime_percent" nullable:"true"`
	// Periods follow one another from TrackedFrom to To, and never two in a row of one status
	Periods []*ProbeAvailabilityPeriod `json:"periods" validate:"required" nullable:"false"`
	// Instances are only in the availability of one probe, where they are a list even when empty,
	// and always cover the last 24 hours. The list of every probe leaves them out.
	Instances *[]*ProbeAvailabilityInstance `json:"instances,omitempty" nullable:"false"`
}

// ToProbeAvailabilityResponse converts an Availability to ProbeAvailabilityResponse. withInstances
// is false for the list of every probe, which leaves the instances out.
func ToProbeAvailabilityResponse(a *Availability, withInstances bool) *ProbeAvailabilityResponse {
	periods := make([]*ProbeAvailabilityPeriod, len(a.Periods))
	for i, p := range a.Periods {
		periods[i] = &ProbeAvailabilityPeriod{Status: string(p.Status), Start: p.Start, End: p.End}
	}
	response := &ProbeAvailabilityResponse{
		ProbeID:       a.ProbeID,
		Range:         string(a.Range),
		From:          a.From,
		To:            a.To,
		InProcess:     a.InProcess,
		TrackedFrom:   a.TrackedFrom,
		UptimePercent: a.UptimePercent,
		Periods:       periods,
	}
	if withInstances {
		instances := make([]*ProbeAvailabilityInstance, len(a.Instances))
		response.Instances = &instances
		for i, instance := range a.Instances {
			connections := make([]*ProbeAvailabilityConnection, len(instance.Connections))
			for j, c := range instance.Connections {
				connections[j] = &ProbeAvailabilityConnection{Start: c.Start, End: c.End, Open: c.Open}
			}
			instances[i] = &ProbeAvailabilityInstance{ID: instance.ID, Hostname: instance.Hostname, Version: instance.Version, Connections: connections}
		}
	}
	return response
}
