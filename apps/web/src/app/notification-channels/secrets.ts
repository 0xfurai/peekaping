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

// The fields of a channel type's config that hold a secret, those that decide where the channel
// sends, and the URLs that can carry credentials, by config key. The server keeps a saved secret
// only while every endpoint field of the config stays as saved, so after an edited endpoint the
// form asks for each saved secret again: typed anew, or removed. The API shows the credentials in
// a URL field as the placeholder, and keeps them only while the URL is sent back exactly as
// shown, so an edited URL that still holds the placeholder is asked for again too.
//
// Keep this as the `secret:"true"`, `endpoint:"true"` and `urlCredentials:"true"` tags on the
// config structs in apps/server/internal/modules/notification_channel/providers say (secrets.test.ts
// checks the URL ones). A field with several tags is in each list. A type that isn't here has none.
type ConfigFieldKeys = { secrets: string[]; endpoints: string[]; urls: string[] };

export const configFieldKeys: Record<string, ConfigFieldKeys> = {
  discord: { secrets: ["webhook_url"], endpoints: ["webhook_url"], urls: [] },
  google_chat: { secrets: ["webhook_url"], endpoints: ["webhook_url"], urls: [] },
  gotify: { secrets: ["application_token"], endpoints: ["server_url"], urls: ["server_url"] },
  grafana_oncall: { secrets: ["grafana_oncall_url"], endpoints: ["grafana_oncall_url"], urls: [] },
  line: { secrets: ["channel_access_token"], endpoints: [], urls: [] },
  matrix: { secrets: ["access_token"], endpoints: ["homeserver_url"], urls: ["homeserver_url"] },
  mattermost: { secrets: ["webhook_url"], endpoints: ["webhook_url"], urls: [] },
  ntfy: { secrets: ["password", "token"], endpoints: ["server_url"], urls: ["server_url"] },
  opsgenie: { secrets: ["api_key"], endpoints: [], urls: [] },
  pagerduty: {
    secrets: ["pagerduty_integration_key"],
    endpoints: ["pagerduty_integration_url"],
    urls: ["pagerduty_integration_url"],
  },
  pagertree: { secrets: ["integrationUrl", "authToken"], endpoints: ["integrationUrl"], urls: [] },
  pushbullet: { secrets: ["pushbullet_access_token"], endpoints: [], urls: [] },
  pushover: { secrets: ["pushover_user_key", "pushover_app_token"], endpoints: [], urls: [] },
  sendgrid: { secrets: ["api_key"], endpoints: [], urls: [] },
  signal: { secrets: [], endpoints: [], urls: ["signal_url"] },
  slack: {
    secrets: ["slack_webhook_url", "slack_bot_token"],
    endpoints: ["slack_webhook_url"],
    urls: [],
  },
  smtp: {
    secrets: ["password"],
    endpoints: ["smtp_secure", "smtp_host", "smtp_port"],
    urls: [],
  },
  teams: { secrets: ["webhook_url"], endpoints: ["webhook_url"], urls: [] },
  telegram: { secrets: ["bot_token"], endpoints: ["server_url"], urls: ["server_url"] },
  twilio: { secrets: ["twilio_api_key", "twilio_auth_token"], endpoints: [], urls: [] },
  webhook: {
    secrets: ["webhook_additional_headers", "webhook_signing_secret"],
    endpoints: ["webhook_url"],
    urls: ["webhook_url"],
  },
  wecom: { secrets: ["webhook_url"], endpoints: ["webhook_url"], urls: [] },
  whatsapp: { secrets: ["api_key"], endpoints: ["server_url"], urls: ["server_url"] },
};

// The type of the field error that asks for a secret again, and the i18n key of its message.
// Saving or testing sets it on each secret still holding the placeholder after an endpoint moved.
export const REENTER_ERROR_TYPE = "savedSecret";
export const REENTER_ERROR_MESSAGE = "notifications.secret.reenter_error";
// The message for a URL field, which holds credentials inside the URL rather than being one
export const REENTER_URL_ERROR_MESSAGE = "notifications.secret.reenter_url_error";

// The field error that asks for the field `key` of a `type` channel again
export function reenterError(type: string, key: string) {
  const url = (configFieldKeys[type]?.urls ?? []).includes(key);
  return { type: REENTER_ERROR_TYPE, message: url ? REENTER_URL_ERROR_MESSAGE : REENTER_ERROR_MESSAGE };
}

// The form values with every saved-secret placeholder emptied, and every value that holds one,
// such as a URL with masked credentials. A placeholder stands for a secret of the saved channel,
// so it means nothing once the form leaves that channel's type: ntfy, Gotify, WhatsApp and
// Telegram share the key `server_url`, and another type's masked URL is no URL of this one.
export function withoutSavedSecrets<T extends Record<string, unknown>>(values: T): T {
  return Object.fromEntries(
    Object.entries(values).map(([key, value]) => [
      key,
      typeof value === "string" && value.includes(SECRET_PLACEHOLDER) ? "" : value,
    ])
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

// The form values as the form sends them. After an endpoint moved, a saved secret whose field
// the form doesn't show can't be asked for, and the server won't keep it: it goes empty. The
// form itself keeps the placeholder, so that the field has it back when it is shown again or the
// endpoint is back as saved. `shown` are the secrets whose fields are on the form.
export function withoutUnaskedSecrets<T extends Record<string, unknown>>(
  values: T,
  saved: Record<string, unknown> | undefined,
  shown: ReadonlySet<string>
): T {
  if (!saved) return values;
  // A URL field is on the form whenever its type is: it isn't one of the `shown` secrets, and
  // is never blanked
  const urls = configFieldKeys[String(values.type)]?.urls ?? [];
  const unasked = secretsToReenter(values, saved).filter(
    (key) => !shown.has(key) && !urls.includes(key)
  );
  if (unasked.length === 0) return values;
  return { ...values, ...Object.fromEntries(unasked.map((key) => [key, ""])) };
}

// The secrets and URLs of the form that still hold the placeholder though the server won't keep
// them, by key. The server won't keep a secret once where the channel sends has changed from
// `saved`, the values the form opened with. It won't keep the credentials of a URL once the URL
// isn't as shown. Each has to be typed again or removed. Nothing is asked for another type than
// the saved one.
export function secretsToReenter(
  values: Record<string, unknown>,
  saved: Record<string, unknown>
): string[] {
  const type = values.type;
  if (typeof type !== "string" || type !== saved.type) return [];
  const keys = configFieldKeys[type];
  const urls = (keys?.urls ?? []).filter((key) => {
    const value = values[key];
    return typeof value === "string" && value.includes(SECRET_PLACEHOLDER) && value !== saved[key];
  });
  const secrets = endpointChanged(type, values, saved)
    ? (keys?.secrets ?? []).filter((key) => values[key] === SECRET_PLACEHOLDER)
    : [];
  return [...secrets, ...urls.filter((key) => !secrets.includes(key))];
}

// The fields of `secretsToReenter` that the form can ask for: a URL is always on the form, a
// secret only while its field is. `shown` are the secrets whose fields are on the form.
export function askedToReenter(
  values: Record<string, unknown>,
  saved: Record<string, unknown>,
  shown: ReadonlySet<string>
): string[] {
  const urls = configFieldKeys[String(values.type)]?.urls ?? [];
  return secretsToReenter(values, saved).filter((key) => urls.includes(key) || shown.has(key));
}
