import { isAxiosError } from "axios";
import type {
  AdminInstallSettingsResponse,
  AdminUpdateAccountSmtpDto,
  AdminUpdateDefaultLimitsDto,
  AdminUpdateInstallSettingsDto,
} from "@/api/types.gen";

// The install settings an instance admin changes on Admin → Settings (docs/adr/0005, section 5):
// the signup mode, the sender of account emails, the public URL and the default limits of a
// workspace (PP-160). Each comes from the server's environment, which locks it, or from the value
// saved here, or from the default. The server decides and refuses; these only shape the form and
// its request.

export type SettingSource = "environment" | "saved" | "default";

// The install's default limits, in the order the page shows them, by their names in the admin API
export const DEFAULT_LIMITS = [
  "monitors",
  "min_check_interval",
  "status_pages",
  "custom_domains",
  "members",
  "owned_workspaces",
  "probes_per_monitor",
  "retention_days",
] as const satisfies readonly (keyof AdminInstallSettingsResponse["workspaceLimits"])[];

export type DefaultLimit = (typeof DEFAULT_LIMITS)[number];

// The key of the save's mutation, so that the form knows while one is pending
export const SAVE_INSTALL_SETTINGS = ["admin", "settings", "save"] as const;

// The form's values. The password is what the admin typed: empty keeps the one there is.
export type InstallSettingsFormValues = {
  signupMode: string;
  publicUrl: string;
  host: string;
  port: number;
  user: string;
  password: string;
  from: string;
  tls: string;
  // Each default limit as typed: blank is none
  workspaceLimits: Record<DefaultLimit, string>;
};

export const installSettingsFormValues = (
  settings: AdminInstallSettingsResponse
): InstallSettingsFormValues => ({
  signupMode: settings.signupMode.value,
  publicUrl: settings.publicUrl.value,
  host: settings.accountSmtp.host.value,
  port: settings.accountSmtp.port.value,
  user: settings.accountSmtp.user.value,
  password: "",
  from: settings.accountSmtp.from.value,
  tls: settings.accountSmtp.tls.value,
  workspaceLimits: Object.fromEntries(
    DEFAULT_LIMITS.map((name) => [name, settings.workspaceLimits[name].value?.toString() ?? ""])
  ) as Record<DefaultLimit, string>,
});

// The request that saves what the admin changed, and nothing else: a field left as it loaded, or
// locked by the environment, is left out, so that a save never touches a setting nobody changed.
// An emptied field is sent empty (a port as 0), which clears the saved value. The password is
// sent when one was typed, or empty when the admin removed the one there is.
export const installSettingsChange = (
  settings: AdminInstallSettingsResponse,
  values: InstallSettingsFormValues,
  { removePassword = false }: { removePassword?: boolean } = {}
): AdminUpdateInstallSettingsDto => {
  const body: AdminUpdateInstallSettingsDto = {};
  const changed = (field: { value: string; locked: boolean }, value: string) =>
    !field.locked && value.trim() !== field.value;

  if (changed(settings.signupMode, values.signupMode)) body.signupMode = values.signupMode;
  if (changed(settings.publicUrl, values.publicUrl)) body.publicUrl = values.publicUrl.trim();

  const smtp = settings.accountSmtp;
  const sender: AdminUpdateAccountSmtpDto = {};
  if (changed(smtp.host, values.host)) sender.host = values.host.trim();
  if (!smtp.port.locked && values.port !== smtp.port.value) sender.port = values.port;
  if (changed(smtp.user, values.user)) sender.user = values.user.trim();
  if (!smtp.password.locked) {
    if (values.password !== "") sender.password = values.password;
    else if (removePassword && smtp.password.set) sender.password = "";
  }
  if (changed(smtp.from, values.from)) sender.from = values.from.trim();
  if (changed(smtp.tls, values.tls)) sender.tls = values.tls;
  if (Object.keys(sender).length > 0) body.accountSmtp = sender;

  // A default limit emptied is sent as null, which clears it: no limit. The form's check keeps a
  // value that is no whole number of 1 or more from being saved.
  const limits: AdminUpdateDefaultLimitsDto = {};
  for (const name of DEFAULT_LIMITS) {
    const setting = settings.workspaceLimits[name];
    const typed = values.workspaceLimits[name].trim();
    if (setting.locked || typed === (setting.value?.toString() ?? "")) continue;
    if (typed === "") limits[name] = null;
    else if (Number.isFinite(Number(typed))) limits[name] = Number(typed);
  }
  if (Object.keys(limits).length > 0) body.workspaceLimits = limits;
  return body;
};

/** Whether a typed default limit can be saved: blank for none, or a whole number of 1 or more. */
export const isDefaultLimit = (typed: string): boolean => {
  const value = typed.trim();
  return value === "" || (/^\d+$/.test(value) && Number(value) >= 1);
};

// Whether saving the change clears the saved password: a change of the host, port, security or
// user that sends no password does, so that the password is never sent to another server
export const clearsSavedPassword = (
  settings: AdminInstallSettingsResponse,
  change: AdminUpdateInstallSettingsDto
): boolean => {
  const sender = change.accountSmtp;
  if (!settings.accountSmtp.password.set || !sender || sender.password !== undefined) return false;
  return [sender.host, sender.port, sender.user, sender.tls].some((value) => value !== undefined);
};

// The translation key of a setting's label, by its name in the admin API
const FIELD_LABELS: Record<string, string> = {
  signupMode: "admin.settings.signup.label",
  publicUrl: "admin.settings.public_url.label",
  "accountSmtp.host": "admin.settings.sender.host",
  "accountSmtp.port": "admin.settings.sender.port",
  "accountSmtp.user": "admin.settings.sender.user",
  "accountSmtp.password": "admin.settings.sender.password",
  "accountSmtp.from": "admin.settings.sender.from",
  "accountSmtp.tls": "admin.settings.sender.tls",
  ...Object.fromEntries(DEFAULT_LIMITS.map((name) => [`workspaceLimits.${name}`, `limits.names.${name}`])),
};

type Translate = (key: string, vars?: Record<string, unknown>) => string;

// The text to show for a refused save or test email: the translated reason for each code of the
// server's SettingErrorResponse (apps/server/internal/modules/admin/admin.settings.dto.go). An
// invalid value keeps the server's reason, after the setting's translated label. Anything else
// shows the server's message, else the fallback.
export const installSettingsErrorMessage = (
  error: unknown,
  t: Translate,
  fallback: string
): string => {
  if (!isAxiosError(error) || !error.response) return fallback;
  const data = error.response.data as
    | { message?: unknown; error?: { code?: unknown; field?: unknown } }
    | undefined;
  const message = typeof data?.message === "string" ? data.message : "";
  const field = typeof data?.error?.field === "string" ? data.error.field : "";
  const label = FIELD_LABELS[field] ? t(FIELD_LABELS[field]) : field;

  switch (data?.error?.code) {
    case "SETTING_LOCKED":
      return t("admin.settings.errors.locked", { field: label });
    case "OPEN_SIGNUP_NEEDS_SENDER":
      return t("admin.settings.errors.open_needs_sender");
    case "SENDER_NEEDED_FOR_OPEN_SIGNUP":
      return t("admin.settings.errors.sender_needed");
    case "NO_SENDER":
      return t("admin.settings.errors.no_sender");
    case "INVALID_SETTING": {
      // The message is "<field>: <reason>"
      const reason = field && message.startsWith(`${field}: `) ? message.slice(field.length + 2) : message;
      return label ? `${label}: ${reason}` : reason || fallback;
    }
    default:
      return message || fallback;
  }
};
