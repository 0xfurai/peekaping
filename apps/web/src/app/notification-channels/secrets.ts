import { z } from "zod";

// What the API shows in place of a secret a channel has saved. A form that sends it back keeps
// the saved secret.
export const SECRET_PLACEHOLDER = "********";

// Accepts the placeholder as well as what `schema` accepts, for a secret field: the edit form
// holds the placeholder until the user types a new secret, and a URL or length check would
// refuse it.
export function withSavedSecret<T extends z.ZodTypeAny>(schema: T) {
  return z.union([z.literal(SECRET_PLACEHOLDER), schema]);
}

// The fields of a channel type's config that hold a secret, and those that decide where the
// channel sends, by config key. The server keeps a saved secret only while every endpoint field
// of the config stays as saved, so an edited endpoint empties the saved secrets of the form.
//
// Keep this as the `secret:"true"` and `endpoint:"true"` tags on the config structs in
// apps/server/internal/modules/notification_channel/providers say. A field with both tags is in
// both lists. A type that isn't here has neither.
export const configFieldKeys: Record<string, { secrets: string[]; endpoints: string[] }> = {
  discord: { secrets: ["webhook_url"], endpoints: ["webhook_url"] },
  google_chat: { secrets: ["webhook_url"], endpoints: ["webhook_url"] },
  gotify: { secrets: ["application_token"], endpoints: ["server_url"] },
  grafana_oncall: { secrets: ["grafana_oncall_url"], endpoints: ["grafana_oncall_url"] },
  line: { secrets: ["channel_access_token"], endpoints: [] },
  matrix: { secrets: ["access_token"], endpoints: ["homeserver_url"] },
  mattermost: { secrets: ["webhook_url"], endpoints: ["webhook_url"] },
  ntfy: { secrets: ["password", "token"], endpoints: ["server_url"] },
  opsgenie: { secrets: ["api_key"], endpoints: [] },
  pagerduty: { secrets: ["pagerduty_integration_key"], endpoints: ["pagerduty_integration_url"] },
  pagertree: { secrets: ["integrationUrl", "authToken"], endpoints: ["integrationUrl"] },
  pushbullet: { secrets: ["pushbullet_access_token"], endpoints: [] },
  pushover: { secrets: ["pushover_user_key", "pushover_app_token"], endpoints: [] },
  sendgrid: { secrets: ["api_key"], endpoints: [] },
  slack: { secrets: ["slack_webhook_url", "slack_bot_token"], endpoints: ["slack_webhook_url"] },
  smtp: { secrets: ["password"], endpoints: ["smtp_secure", "smtp_host", "smtp_port"] },
  teams: { secrets: ["webhook_url"], endpoints: ["webhook_url"] },
  telegram: { secrets: ["bot_token"], endpoints: ["server_url"] },
  twilio: { secrets: ["twilio_api_key", "twilio_auth_token"], endpoints: [] },
  webhook: {
    secrets: ["webhook_additional_headers", "webhook_signing_secret"],
    endpoints: ["webhook_url"],
  },
  wecom: { secrets: ["webhook_url"], endpoints: ["webhook_url"] },
  whatsapp: { secrets: ["api_key"], endpoints: ["server_url"] },
};

// The form values with every saved-secret placeholder emptied. A placeholder stands for a secret
// of the saved channel, so it means nothing once the form leaves that channel's type.
export function withoutSavedSecrets<T extends Record<string, unknown>>(values: T): T {
  return Object.fromEntries(
    Object.entries(values).map(([key, value]) => [key, value === SECRET_PLACEHOLDER ? "" : value])
  ) as T;
}

// Whether any endpoint field of the type's config has a value other than the saved one. A secret
// endpoint holds the placeholder while it is as saved.
export function endpointChanged(
  type: string,
  values: Record<string, unknown>,
  saved: Record<string, unknown>
): boolean {
  return (configFieldKeys[type]?.endpoints ?? []).some(
    (key) => String(values[key] ?? "") !== String(saved[key] ?? "")
  );
}
