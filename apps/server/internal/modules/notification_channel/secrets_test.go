package notification_channel

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	if s == "" {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("not JSON: %q: %v", s, err)
	}
	return out
}

func TestIsSecretKey(t *testing.T) {
	secret := []string{
		"password", "smtp_password", "token", "bot_token", "slack_bot_token",
		"webhook_url", "slack_webhook_url", "api_key", "twilio_auth_token",
		"pushover_user_key", "pagerduty_integration_key", "webhook_signing_secret",
		"webhook_additional_headers", "grafana_oncall_url",
	}
	for _, key := range secret {
		if !IsSecretKey(key) {
			t.Errorf("%q should be a secret", key)
		}
	}

	public := []string{
		"smtp_host", "smtp_port", "from", "to", "server_url", "topic",
		"username", "priority", "slack_channel", "homeserver_url", "signal_url",
	}
	for _, key := range public {
		if IsSecretKey(key) {
			t.Errorf("%q should not be a secret", key)
		}
	}
}

func TestSplitSecretsMovesCredentialsOutOfConfig(t *testing.T) {
	config := `{"smtp_host":"mail.example.com","smtp_port":465,"username":"u","password":"p4ss"}`
	clean, secret, err := SplitSecrets(config, "")
	if err != nil {
		t.Fatal(err)
	}

	gotConfig := decode(t, clean)
	if _, ok := gotConfig["password"]; ok {
		t.Error("password stayed in config")
	}
	if gotConfig["smtp_host"] != "mail.example.com" {
		t.Errorf("config lost a public field: %v", gotConfig)
	}
	if decode(t, secret)["password"] != "p4ss" {
		t.Errorf("secret did not receive the password: %q", secret)
	}
}

func TestSplitSecretsKeepsStoredSecretWhenTheClientSendsNothing(t *testing.T) {
	// Editing a channel in the UI sends the form back without the password.
	clean, secret, err := SplitSecrets(`{"smtp_host":"mail.example.com","password":""}`, `{"password":"stored"}`)
	if err != nil {
		t.Fatal(err)
	}
	if decode(t, secret)["password"] != "stored" {
		t.Errorf("stored secret was lost: %q", secret)
	}
	if _, ok := decode(t, clean)["password"]; ok {
		t.Error("an empty password stayed in config")
	}
}

func TestSplitSecretsReplacesStoredSecret(t *testing.T) {
	_, secret, err := SplitSecrets(`{"password":"new"}`, `{"password":"old","token":"keepme"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, secret)
	if got["password"] != "new" {
		t.Errorf("password not replaced: %v", got)
	}
	if got["token"] != "keepme" {
		t.Errorf("unrelated stored secret was dropped: %v", got)
	}
}

func TestSplitSecretsWithNoSecretsLeavesConfigAlone(t *testing.T) {
	clean, secret, err := SplitSecrets(`{"server_url":"https://ntfy.example.com","topic":"alerts"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" {
		t.Errorf("expected no secret, got %q", secret)
	}
	if decode(t, clean)["topic"] != "alerts" {
		t.Errorf("config changed: %q", clean)
	}
}

func TestMergeSecretPutsCredentialsBack(t *testing.T) {
	merged := MergeSecret(`{"smtp_host":"mail.example.com"}`, `{"password":"p4ss"}`)
	got := decode(t, merged)
	if got["password"] != "p4ss" || got["smtp_host"] != "mail.example.com" {
		t.Errorf("merge lost a field: %v", got)
	}
}

func TestMergeSecretWithoutASecretReturnsConfig(t *testing.T) {
	config := `{"a":1}`
	if got := MergeSecret(config, ""); got != config {
		t.Errorf("got %q, want %q", got, config)
	}
}

func TestRedactHidesCredentialsAlreadyInConfig(t *testing.T) {
	config := `{"smtp_host":"mail.example.com","password":"p4ss"}`
	got := decode(t, *Redact(&config))
	if _, ok := got["password"]; ok {
		t.Error("password survived redaction")
	}
	if got["smtp_host"] != "mail.example.com" {
		t.Error("redaction dropped a public field")
	}
}

func TestRedactLeavesAFreeConfigUntouched(t *testing.T) {
	config := `{"server_url":"https://ntfy.example.com"}`
	if got := Redact(&config); got == nil || *got != config {
		t.Errorf("config was rewritten: %v", got)
	}
}

func TestWriteSecretsExplicitSecretWins(t *testing.T) {
	clean, secret, err := WriteSecrets(`{"password":"in-config"}`, `{"password":"explicit"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	if decode(t, secret)["password"] != "explicit" {
		t.Errorf("explicit secret did not win: %q", secret)
	}
	if _, ok := decode(t, clean)["password"]; ok {
		t.Error("password stayed in config")
	}
}

func TestWriteSecretsKeepsAnUnparseableConfig(t *testing.T) {
	// Channels created before this change may hold something that is not JSON. Editing
	// one must not fail because of it.
	config, secret, err := WriteSecrets("config-string", "", `{"password":"stored"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config != "config-string" {
		t.Errorf("config was rewritten: %q", config)
	}
	if secret != `{"password":"stored"}` {
		t.Errorf("stored secret was lost: %q", secret)
	}
}

func TestWriteSecretsRejectsABrokenExplicitSecret(t *testing.T) {
	if _, _, err := WriteSecrets("{}", "{not json", ""); err == nil {
		t.Error("expected an error for a broken explicit secret")
	}
}

func TestFullConfigMergesForTheProvider(t *testing.T) {
	config := `{"smtp_host":"mail.example.com"}`
	secret := `{"password":"p4ss"}`
	model := &Model{Config: &config, Secret: &secret}
	if decode(t, model.FullConfig())["password"] != "p4ss" {
		t.Errorf("provider did not get the secret: %q", model.FullConfig())
	}
}
