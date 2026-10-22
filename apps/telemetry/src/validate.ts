// The usage ping contract (ticket #232, docs/adr/0003-telemetry-and-error-reporting.md).
// A strict allowlist: every field is required, every value is checked, anything else is rejected.
// The error is a fixed string naming only a contract field; it never repeats a key or value from the input.

export const MODES = ["single", "cluster"] as const;
export const DBS = ["sqlite", "postgres", "mongo", "mysql"] as const;
export const MONITOR_BUCKETS = ["0", "1-10", "11-50", "51-200", "201-1000", "1000+"] as const;
export const FEATURE_KEYS = ["status_pages", "maintenance", "metrics"] as const;
export const MAX_TYPES = 50;

export interface Ping {
  instance_id: string;
  version: string;
  mode: (typeof MODES)[number];
  db: (typeof DBS)[number];
  os: string;
  monitors: (typeof MONITOR_BUCKETS)[number];
  monitor_types: string[];
  // The notification channel types in use. The key keeps the name servers send (the wire contract).
  notification_providers: string[];
  features: { status_pages: boolean; maintenance: boolean; metrics: boolean };
}

export type ValidationResult = { ok: true; ping: Ping } | { ok: false; error: string };

const FIELDS = [
  "instance_id",
  "version",
  "mode",
  "db",
  "os",
  "monitors",
  "monitor_types",
  "notification_providers",
  "features",
] as const;

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const VERSION = /^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/;
const OS = /^[a-z0-9]+\/[a-z0-9]+$/;
const TYPE_NAME = /^[a-z0-9_-]{1,32}$/;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExactKeys(value: Record<string, unknown>, keys: readonly string[]): boolean {
  const own = Object.keys(value);
  return own.length === keys.length && keys.every((key) => Object.hasOwn(value, key));
}

function isOneOf<T extends string>(value: unknown, allowed: readonly T[]): value is T {
  return typeof value === "string" && (allowed as readonly string[]).includes(value);
}

function matches(value: unknown, pattern: RegExp, maxLength: number): value is string {
  return typeof value === "string" && value.length <= maxLength && pattern.test(value);
}

function isTypeList(value: unknown): value is string[] {
  return (
    Array.isArray(value) &&
    value.length <= MAX_TYPES &&
    value.every((entry) => matches(entry, TYPE_NAME, 32)) &&
    new Set(value).size === value.length
  );
}

export function validatePing(input: unknown): ValidationResult {
  if (!isRecord(input)) return { ok: false, error: "payload is not an object" };
  if (!hasExactKeys(input, FIELDS)) return { ok: false, error: "payload fields differ from the contract" };

  const { instance_id, version, mode, db, os, monitors, monitor_types, notification_providers, features } = input;

  if (!matches(instance_id, UUID_V4, 36)) return { ok: false, error: "invalid instance_id" };
  if (!matches(version, VERSION, 32)) return { ok: false, error: "invalid version" };
  if (!isOneOf(mode, MODES)) return { ok: false, error: "invalid mode" };
  if (!isOneOf(db, DBS)) return { ok: false, error: "invalid db" };
  if (!matches(os, OS, 32)) return { ok: false, error: "invalid os" };
  if (!isOneOf(monitors, MONITOR_BUCKETS)) return { ok: false, error: "invalid monitors" };
  if (!isTypeList(monitor_types)) return { ok: false, error: "invalid monitor_types" };
  if (!isTypeList(notification_providers)) return { ok: false, error: "invalid notification_providers" };

  if (!isRecord(features) || !hasExactKeys(features, FEATURE_KEYS)) return { ok: false, error: "invalid features" };
  const { status_pages, maintenance, metrics } = features;
  if (typeof status_pages !== "boolean" || typeof maintenance !== "boolean" || typeof metrics !== "boolean") {
    return { ok: false, error: "invalid features" };
  }

  return {
    ok: true,
    ping: {
      instance_id,
      version,
      mode,
      db,
      os,
      monitors,
      monitor_types: [...monitor_types],
      notification_providers: [...notification_providers],
      features: { status_pages, maintenance, metrics },
    },
  };
}
