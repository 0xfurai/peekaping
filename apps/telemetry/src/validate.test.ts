import { describe, expect, test } from "bun:test";
import { validatePing } from "./validate";

// The payload contract from ticket #232: the server sends exactly this.
function example(): Record<string, unknown> {
  return {
    instance_id: "0b8f6c1e-5d3a-4b7e-9c2a-1f4e8d7a6b50",
    version: "0.0.48",
    mode: "single",
    db: "sqlite",
    os: "linux/arm64",
    monitors: "11-50",
    monitor_types: ["http", "ping", "docker"],
    notification_providers: ["telegram", "slack"],
    features: { status_pages: true, maintenance: false, metrics: true },
  };
}

function withField(field: string, value: unknown): Record<string, unknown> {
  return { ...example(), [field]: value };
}

function without(field: string): Record<string, unknown> {
  const payload = example();
  delete payload[field];
  return payload;
}

function withFeatures(features: unknown): Record<string, unknown> {
  return withField("features", features);
}

describe("validatePing accepts", () => {
  test("the example payload from the contract, unchanged", () => {
    const result = validatePing(example());
    expect(result as unknown).toEqual({ ok: true, ping: example() });
  });

  test("a cluster-mode Postgres install with nothing configured", () => {
    const payload = {
      ...example(),
      mode: "cluster",
      db: "postgres",
      monitors: "0",
      monitor_types: [],
      notification_providers: [],
      features: { status_pages: false, maintenance: false, metrics: false },
    };
    expect(validatePing(payload).ok).toBe(true);
  });

  test.each(["0", "1-10", "11-50", "51-200", "201-1000", "1000+"])("the monitors bucket %s", (bucket) => {
    expect(validatePing(withField("monitors", bucket)).ok).toBe(true);
  });

  test.each(["single", "cluster"])("the mode %s", (mode) => {
    expect(validatePing(withField("mode", mode)).ok).toBe(true);
  });

  test.each(["sqlite", "postgres", "mongo"])("the storage backend %s", (db) => {
    expect(validatePing(withField("db", db)).ok).toBe(true);
  });

  test.each(["0.0.48", "1.22.333", "0.0.48-rc.1", "1.0.0-beta-2", "10.0.0-0"])("the version %s", (version) => {
    expect(validatePing(withField("version", version)).ok).toBe(true);
  });

  test.each(["linux/amd64", "darwin/arm64", "windows/amd64", "linux/386"])("the os %s", (os) => {
    expect(validatePing(withField("os", os)).ok).toBe(true);
  });

  test("50 monitor types and 50 notification channel types, the largest allowed", () => {
    const fifty = Array.from({ length: 50 }, (_, i) => `type_${i}`);
    const payload = { ...example(), monitor_types: fifty, notification_providers: fifty };
    expect(validatePing(payload).ok).toBe(true);
  });

  test("type names of 1 and 32 characters, with digits, underscores and dashes", () => {
    const payload = { ...example(), monitor_types: ["a", "a_b-c9", "a".repeat(32)] };
    expect(validatePing(payload).ok).toBe(true);
  });

  test("a version and an os of exactly 32 characters", () => {
    const version = `1.2.3-${"a".repeat(26)}`;
    const os = `${"a".repeat(15)}/${"b".repeat(16)}`;
    expect(version.length).toBe(32);
    expect(os.length).toBe(32);
    expect(validatePing({ ...example(), version, os }).ok).toBe(true);
  });
});

describe("validatePing rejects", () => {
  test.each([
    ["null", null],
    ["an array", [example()]],
    ["a string", "ping"],
    ["a number", 1],
    ["undefined", undefined],
  ])("a payload that is %s", (_name, payload) => {
    expect(validatePing(payload).ok).toBe(false);
  });

  test.each(Object.keys(example()))("a payload without %s", (field) => {
    expect(validatePing(without(field)).ok).toBe(false);
  });

  test("an extra top-level field", () => {
    expect(validatePing(withField("country", "UA")).ok).toBe(false);
  });

  test("an own __proto__ field, as JSON.parse produces it", () => {
    const payload = JSON.parse(`{"__proto__": {"admin": true}, ${JSON.stringify(example()).slice(1)}`);
    expect(validatePing(payload).ok).toBe(false);
  });

  test("an extra field in features", () => {
    expect(validatePing(withFeatures({ status_pages: true, maintenance: false, metrics: true, tags: true })).ok).toBe(
      false,
    );
  });

  test.each(["status_pages", "maintenance", "metrics"])("features without %s", (key) => {
    const features: Record<string, unknown> = { status_pages: true, maintenance: false, metrics: true };
    delete features[key];
    expect(validatePing(withFeatures(features)).ok).toBe(false);
  });

  test.each([
    ["a string", "true"],
    ["a number", 1],
    ["null", null],
    ["an array", [true]],
  ])("a feature flag that is %s", (_name, value) => {
    expect(validatePing(withFeatures({ status_pages: value, maintenance: false, metrics: true })).ok).toBe(false);
  });

  test.each([
    ["null", null],
    ["an array", []],
    ["a string", "all"],
  ])("features that are %s", (_name, value) => {
    expect(validatePing(withFeatures(value)).ok).toBe(false);
  });

  test.each([
    ["upper case", "0B8F6C1E-5D3A-4B7E-9C2A-1F4E8D7A6B50"],
    ["a version 1 UUID", "0b8f6c1e-5d3a-1b7e-9c2a-1f4e8d7a6b50"],
    ["a wrong variant", "0b8f6c1e-5d3a-4b7e-1c2a-1f4e8d7a6b50"],
    ["no dashes", "0b8f6c1e5d3a4b7e9c2a1f4e8d7a6b50"],
    ["trailing text", "0b8f6c1e-5d3a-4b7e-9c2a-1f4e8d7a6b50x"],
    ["a trailing newline", "0b8f6c1e-5d3a-4b7e-9c2a-1f4e8d7a6b50\n"],
    ["empty", ""],
    ["a number", 1],
  ])("an instance_id that is %s", (_name, value) => {
    expect(validatePing(withField("instance_id", value)).ok).toBe(false);
  });

  test.each([
    ["two parts", "1.2"],
    ["a v prefix", "v1.2.3"],
    ["an empty prerelease", "1.2.3-"],
    ["build metadata", "1.2.3+build"],
    ["four parts", "1.2.3.4"],
    ["letters", "1.2.x"],
    ["a trailing newline", "0.0.48\n"],
    ["33 characters", `1.2.3-${"a".repeat(27)}`],
    ["empty", ""],
    ["a number", 1.2],
  ])("a version with %s", (_name, value) => {
    expect(validatePing(withField("version", value)).ok).toBe(false);
  });

  test.each(["", "Single", "multi", "SINGLE", 1, null])("the mode %p", (mode) => {
    expect(validatePing(withField("mode", mode)).ok).toBe(false);
  });

  test.each(["", "mysql", "mongodb", "SQLite", "postgresql", 1, null])("the storage backend %p", (db) => {
    expect(validatePing(withField("db", db)).ok).toBe(false);
  });

  test.each([
    "Linux/arm64",
    "linux",
    "linux/",
    "/arm64",
    "linux/arm64/v8",
    "linux arm64",
    "linux/arm64\n",
    `${"a".repeat(16)}/${"b".repeat(16)}`,
    "",
    1,
  ])("the os %p", (os) => {
    expect(validatePing(withField("os", os)).ok).toBe(false);
  });

  test.each(["", "2-5", "10", "1000", "1001+", "0-0", "1 - 10", 5, null])("the monitors bucket %p", (bucket) => {
    expect(validatePing(withField("monitors", bucket)).ok).toBe(false);
  });

  test.each(["monitor_types", "notification_providers"])("%s that is not an array", (field) => {
    expect(validatePing(withField(field, "http")).ok).toBe(false);
    expect(validatePing(withField(field, null)).ok).toBe(false);
    expect(validatePing(withField(field, { 0: "http" })).ok).toBe(false);
  });

  test.each(["monitor_types", "notification_providers"])("%s with more than 50 entries", (field) => {
    const fiftyOne = Array.from({ length: 51 }, (_, i) => `type_${i}`);
    expect(validatePing(withField(field, fiftyOne)).ok).toBe(false);
  });

  test.each(["monitor_types", "notification_providers"])("%s with a duplicate entry", (field) => {
    expect(validatePing(withField(field, ["http", "ping", "http"])).ok).toBe(false);
  });

  test.each(["monitor_types", "notification_providers"])("%s with an entry that is not allowed", (field) => {
    for (const entry of ["HTTP", "my monitor", "http\n", "a".repeat(33), "", "é", "a.b", "a/b", 1, null, ["http"]]) {
      expect(validatePing(withField(field, ["ping", entry])).ok).toBe(false);
    }
  });

  test("an error that names no input", () => {
    const secret = "203.0.113.7-secret";
    const payloads = [
      withField(secret, 1),
      withField("version", secret),
      withField("monitor_types", [secret]),
      withFeatures({ status_pages: true, maintenance: false, metrics: true, [secret]: true }),
    ];
    for (const payload of payloads) {
      const result = validatePing(payload);
      expect(result.ok).toBe(false);
      expect(JSON.stringify(result)).not.toContain(secret);
    }
  });
});
