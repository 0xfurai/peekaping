package admin

import (
	"encoding/json"

	"peekaping/internal/httpx"
	"peekaping/internal/installsettings"

	"github.com/danielgtaylor/huma/v2"
)

// StringSettingResponse is an install setting's value and where it comes from
type StringSettingResponse struct {
	Value string `json:"value"`
	// Source is where the value comes from: the server's environment, saved here, or the default
	Source string `json:"source" enum:"environment,saved,default" doc:"Where the value comes from: the server's environment, saved in the admin area, or the default."`
	// Locked is true when the environment sets the value: it can't be changed here
	Locked bool `json:"locked" doc:"Whether the server's environment sets the value. A locked setting can't be changed here."`
	// EnvVar is the variable that sets it
	EnvVar string `json:"envVar" doc:"The environment variable that sets it, and locks it when set." example:"SIGNUP_MODE"`
}

// IntSettingResponse is StringSettingResponse for a number
type IntSettingResponse struct {
	Value  int    `json:"value"`
	Source string `json:"source" enum:"environment,saved,default" doc:"Where the value comes from: the server's environment, saved in the admin area, or the default."`
	Locked bool   `json:"locked" doc:"Whether the server's environment sets the value. A locked setting can't be changed here."`
	EnvVar string `json:"envVar" doc:"The environment variable that sets it, and locks it when set." example:"ACCOUNT_SMTP_PORT"`
}

// SecretSettingResponse is a setting whose value is never shown: only whether one is set
type SecretSettingResponse struct {
	Set    bool   `json:"set" doc:"Whether a value is set. The value itself is never returned."`
	Source string `json:"source" enum:"environment,saved,default" doc:"Where the value comes from: the server's environment, saved in the admin area, or the default (none)."`
	Locked bool   `json:"locked" doc:"Whether the server's environment sets the value. A locked setting can't be changed here."`
	EnvVar string `json:"envVar" doc:"The environment variable that sets it, and locks it when set." example:"ACCOUNT_SMTP_PASSWORD"`
}

// LimitSettingResponse is a default limit: a number, or null for none, and where it comes from
type LimitSettingResponse struct {
	Value  *int   `json:"value" nullable:"true" doc:"The limit, or null for none."`
	Source string `json:"source" enum:"environment,saved,default" doc:"Where the value comes from: the server's environment, saved in the admin area, or the default (none)."`
	Locked bool   `json:"locked" doc:"Whether the server's environment sets the value. A locked setting can't be changed here."`
	EnvVar string `json:"envVar" doc:"The environment variable that sets it, and locks it when set." example:"WORKSPACE_LIMIT_MONITORS"`
}

// DefaultLimitsSettingsResponse are the install's default limits (PP-160): the limits of a
// workspace that has none of its own, by the names a workspace's own limits have
type DefaultLimitsSettingsResponse struct {
	Monitors         LimitSettingResponse `json:"monitors" doc:"How many active monitors a workspace may have."`
	MinCheckInterval LimitSettingResponse `json:"min_check_interval" doc:"The shortest check interval and retry interval of a workspace's monitors, in seconds. MIN_CHECK_INTERVAL applies as well: the stricter wins."`
	StatusPages      LimitSettingResponse `json:"status_pages" doc:"How many status pages a workspace may have."`
	CustomDomains    LimitSettingResponse `json:"custom_domains" doc:"How many custom domains a workspace's status pages may have, together."`
	Members          LimitSettingResponse `json:"members" doc:"How many members a workspace may have, with its pending invites counted."`
	OwnedWorkspaces  LimitSettingResponse `json:"owned_workspaces" doc:"How many workspaces one user may own, counted against this default alone."`
	ProbesPerMonitor LimitSettingResponse `json:"probes_per_monitor" doc:"How many probes one monitor may run on."`
	RetentionDays    LimitSettingResponse `json:"retention_days" doc:"The longest data retention period a workspace may keep, in days: the hourly cleanup deletes a workspace's heartbeats past it when the workspace's own period is longer."`
}

// AccountSMTPSettingsResponse is the sender of account emails
type AccountSMTPSettingsResponse struct {
	Host     StringSettingResponse `json:"host"`
	Port     IntSettingResponse    `json:"port"`
	User     StringSettingResponse `json:"user"`
	Password SecretSettingResponse `json:"password"`
	From     StringSettingResponse `json:"from"`
	// TLS is starttls, tls or none
	TLS StringSettingResponse `json:"tls"`
}

// InstallSettingsResponse is every install setting an instance admin can change here
type InstallSettingsResponse struct {
	// SignupMode is invite or open
	SignupMode  StringSettingResponse       `json:"signupMode"`
	PublicURL   StringSettingResponse       `json:"publicUrl"`
	AccountSMTP AccountSMTPSettingsResponse `json:"accountSmtp"`
	// AccountEmails is whether the install has a sender for account emails now
	AccountEmails bool `json:"accountEmails" doc:"Whether the install has a sender for account emails: an SMTP host is set."`
	// WorkspaceLimits are the install's default limits
	WorkspaceLimits DefaultLimitsSettingsResponse `json:"workspaceLimits"`
}

func stringSetting(f installsettings.Field[string]) StringSettingResponse {
	return StringSettingResponse{Value: f.Value, Source: string(f.Source), Locked: f.Locked(), EnvVar: f.EnvVar}
}

func limitSetting(f installsettings.Field[*int]) LimitSettingResponse {
	return LimitSettingResponse{Value: f.Value, Source: string(f.Source), Locked: f.Locked(), EnvVar: f.EnvVar}
}

func installSettingsResponse(v *installsettings.View) *InstallSettingsResponse {
	l := v.WorkspaceLimits
	return &InstallSettingsResponse{
		SignupMode: stringSetting(v.SignupMode),
		PublicURL:  stringSetting(v.PublicURL),
		AccountSMTP: AccountSMTPSettingsResponse{
			Host:     stringSetting(v.SMTPHost),
			Port:     IntSettingResponse{Value: v.SMTPPort.Value, Source: string(v.SMTPPort.Source), Locked: v.SMTPPort.Locked(), EnvVar: v.SMTPPort.EnvVar},
			User:     stringSetting(v.SMTPUser),
			Password: SecretSettingResponse{Set: v.SMTPPassword.Value, Source: string(v.SMTPPassword.Source), Locked: v.SMTPPassword.Locked(), EnvVar: v.SMTPPassword.EnvVar},
			From:     stringSetting(v.SMTPFrom),
			TLS:      stringSetting(v.SMTPTLS),
		},
		AccountEmails: v.SenderSet(),
		WorkspaceLimits: DefaultLimitsSettingsResponse{
			Monitors:         limitSetting(l.Monitors),
			MinCheckInterval: limitSetting(l.MinCheckInterval),
			StatusPages:      limitSetting(l.StatusPages),
			CustomDomains:    limitSetting(l.CustomDomains),
			Members:          limitSetting(l.Members),
			OwnedWorkspaces:  limitSetting(l.OwnedWorkspaces),
			ProbesPerMonitor: limitSetting(l.ProbesPerMonitor),
			RetentionDays:    limitSetting(l.RetentionDays),
		},
	}
}

// UpdateAccountSMTPDto changes the sender of account emails. A field left out stays as it is; an
// empty string (a port of 0) clears the saved value, so that the setting falls back to its default.
type UpdateAccountSMTPDto struct {
	Host *string `json:"host,omitempty" doc:"The SMTP server's host. Empty clears it, which turns account emails off."`
	Port *int    `json:"port,omitempty" doc:"The SMTP server's port, 1 to 65535. 0 clears it (587)." minimum:"0" maximum:"65535"`
	User *string `json:"user,omitempty" doc:"The user to sign in as. Empty signs in as nobody."`
	// Password is write-only: it is never returned. Left out it stays, unless the request changes
	// the host, port, TLS mode or user, which clears it; empty clears it.
	Password *string `json:"password,omitempty" doc:"The password. Write-only: it is never returned. Left out it stays as it is, unless the request changes the host, port, TLS mode or user: then the saved password is cleared, so that it is never sent to another server. Empty clears it."`
	From     *string `json:"from,omitempty" doc:"The address account emails come from, such as no-reply@example.com or Peekaping <no-reply@example.com>. Required with a host."`
	TLS      *string `json:"tls,omitempty" doc:"How the connection is secured: starttls, tls or none. Empty clears it (starttls)."`
}

// LimitChange is one default limit in a change: a number saves it, null clears it, so that there is
// no limit, and a limit left out stays as it is
type LimitChange struct {
	// sent is whether the request names the limit, as a number or as null
	sent  bool
	value *int
}

func (c *LimitChange) UnmarshalJSON(data []byte) error {
	c.sent = true
	return json.Unmarshal(data, &c.value)
}

func (LimitChange) Schema(huma.Registry) *huma.Schema {
	minimum := 1.0
	return &huma.Schema{Type: "integer", Format: "int64", Nullable: true, Minimum: &minimum}
}

// UpdateDefaultLimitsDto changes the install's default limits. A limit left out stays as it is.
type UpdateDefaultLimitsDto struct {
	Monitors         LimitChange `json:"monitors,omitempty" doc:"How many active monitors a workspace may have, 1 or more. Null clears it: no limit."`
	MinCheckInterval LimitChange `json:"min_check_interval,omitempty" doc:"The shortest check interval of a workspace's monitors, in seconds: at least MIN_CHECK_INTERVAL. Null clears it: no limit."`
	StatusPages      LimitChange `json:"status_pages,omitempty" doc:"How many status pages a workspace may have, 1 or more. Null clears it: no limit."`
	CustomDomains    LimitChange `json:"custom_domains,omitempty" doc:"How many custom domains a workspace's status pages may have together, 1 or more. Null clears it: no limit."`
	Members          LimitChange `json:"members,omitempty" doc:"How many members a workspace may have, with its pending invites, 1 or more. Null clears it: no limit."`
	OwnedWorkspaces  LimitChange `json:"owned_workspaces,omitempty" doc:"How many workspaces one user may own, 1 or more. Null clears it: no limit."`
	ProbesPerMonitor LimitChange `json:"probes_per_monitor,omitempty" doc:"How many probes one monitor may run on, 1 or more. Null clears it: no limit."`
	RetentionDays    LimitChange `json:"retention_days,omitempty" doc:"The longest data retention period a workspace may keep, 1 to 36500 days: the hourly cleanup deletes a workspace's heartbeats past it when the workspace's own period is longer. Null clears it: no limit." maximum:"36500"`
}

// change is the limits the request names, by name: nil for one it clears
func (d UpdateDefaultLimitsDto) change() map[string]*int {
	change := map[string]*int{}
	for name, limit := range map[string]LimitChange{
		installsettings.LimitMonitors: d.Monitors, installsettings.LimitMinCheckInterval: d.MinCheckInterval,
		installsettings.LimitStatusPages: d.StatusPages, installsettings.LimitCustomDomains: d.CustomDomains,
		installsettings.LimitMembers: d.Members, installsettings.LimitOwnedWorkspaces: d.OwnedWorkspaces,
		installsettings.LimitProbesPerMonitor: d.ProbesPerMonitor, installsettings.LimitRetentionDays: d.RetentionDays,
	} {
		if limit.sent {
			change[name] = limit.value
		}
	}
	return change
}

// UpdateInstallSettingsDto is a partial change of the install settings
type UpdateInstallSettingsDto struct {
	SignupMode      *string                 `json:"signupMode,omitempty" doc:"invite or open. Open needs a sender. Empty clears it (invite)."`
	PublicURL       *string                 `json:"publicUrl,omitempty" doc:"The web app's address, which the links in emails and notifications open. Empty clears it (the default)."`
	AccountSMTP     *UpdateAccountSMTPDto   `json:"accountSmtp,omitempty"`
	WorkspaceLimits *UpdateDefaultLimitsDto `json:"workspaceLimits,omitempty" doc:"The install's default limits, the limits of a workspace that has none of its own. A limit left out stays as it is, a number saves it and null clears it."`
}

func (d UpdateInstallSettingsDto) change() installsettings.Change {
	change := installsettings.Change{SignupMode: d.SignupMode, PublicURL: d.PublicURL}
	if smtp := d.AccountSMTP; smtp != nil {
		change.SMTPHost, change.SMTPPort, change.SMTPUser = smtp.Host, smtp.Port, smtp.User
		change.SMTPPassword, change.SMTPFrom, change.SMTPTLS = smtp.Password, smtp.From, smtp.TLS
	}
	if d.WorkspaceLimits != nil {
		change.WorkspaceLimits = d.WorkspaceLimits.change()
	}
	return change
}

// SendTestEmailDto is the address a test email goes to
type SendTestEmailDto struct {
	To string `json:"to" doc:"The address to send the test email to." validate:"required,email"`
}

// SettingErrorResponse is a refused change of the install settings, or a test email with no sender.
// The code tells a client which: SETTING_LOCKED (409) for a setting the server's environment sets,
// INVALID_SETTING for a value the setting can't hold, OPEN_SIGNUP_NEEDS_SENDER for open signup
// without a sender, SENDER_NEEDED_FOR_OPEN_SIGNUP for removing the sender while signup is open,
// and NO_SENDER for a test email on an install with no sender.
type SettingErrorResponse struct {
	Message string           `json:"message"`
	Error   SettingErrorBody `json:"error"`
}

// SettingErrorBody is the code of a SettingErrorResponse and the setting it is about
type SettingErrorBody struct {
	Code string `json:"code" enum:"SETTING_LOCKED,INVALID_SETTING,OPEN_SIGNUP_NEEDS_SENDER,SENDER_NEEDED_FOR_OPEN_SIGNUP,NO_SENDER" example:"SETTING_LOCKED"`
	// Field is the setting, as the request names it, or empty
	Field string `json:"field,omitempty" doc:"The setting the refusal is about, as the request names it: signupMode, publicUrl, accountSmtp.host, workspaceLimits.monitors and the like." example:"signupMode"`
}

type GetInstallSettingsInput = httpx.Input[httpx.NoBody, struct{}]
type UpdateInstallSettingsInput = httpx.Input[UpdateInstallSettingsDto, struct{}]
type SendTestEmailInput = httpx.Input[SendTestEmailDto, struct{}]
