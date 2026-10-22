package probe

import "time"

// CreateProbeDto represents the request to create a probe
type CreateProbeDto struct {
	Name     string `json:"name" validate:"required,min=1,max=255"`
	Location string `json:"location,omitempty" validate:"omitempty,max=255"`
	// NotificationIDs are the notification channels that hear about the probe
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
	ID              string    `json:"id" validate:"required"`
	Hostname        string    `json:"hostname" validate:"required"`
	OS              string    `json:"os" validate:"required"`
	Arch            string    `json:"arch" validate:"required"`
	Version         string    `json:"version" validate:"required"`
	ProtocolVersion int       `json:"protocol_version" validate:"required"`
	LastSeenAt      time.Time `json:"last_seen_at" validate:"required"`
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
		instances[i] = &ProbeInstanceResponse{
			ID:              instance.ID,
			Hostname:        instance.Hostname,
			OS:              instance.OS,
			Arch:            instance.Arch,
			Version:         instance.Version,
			ProtocolVersion: instance.ProtocolVersion,
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
