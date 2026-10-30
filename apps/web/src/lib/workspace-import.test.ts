import { describe, expect, it } from "vitest";
import { AxiosError, AxiosHeaders, type AxiosResponse } from "axios";
import en from "@/i18n/locales/en-US.json";
import {
  changeMessage,
  createdLines,
  affectedByImport,
  importProblems,
  latestOnly,
  MAX_IMPORT_BYTES,
  problemLine,
  readImportFile,
} from "./workspace-import";

// A translate function over the English file, as i18next fills {{name}} in
const t = (key: string, vars: Record<string, unknown> = {}): string => {
  const found = key.split(".").reduce<unknown>((node, part) => (node as Record<string, unknown>)?.[part], en);
  if (typeof found !== "string") return `MISSING ${key}`;
  return found.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(vars[name] ?? `{{${name}}}`));
};

const change = (kind: string, rest: Record<string, string> = {}) => ({ kind, record: "monitor" as const, ...rest });

describe("changeMessage", () => {
  it.each([
    ["tag_reused", { name: "prod" }, 'The tag "prod" is already in this workspace, so its monitors use it.'],
    ["proxy_skipped_target_not_allowed", { name: "10.0.0.5:3128" }, "The proxy 10.0.0.5:3128 was left out: this install blocks its address."],
    ["monitor_proxy_dropped", { name: "API", from: "10.0.0.5:3128" }, 'The monitor "API" comes in without its proxy 10.0.0.5:3128, which was left out.'],
    ["monitor_skipped_type_disabled", { name: "Docs", reason: "ping" }, 'The monitor "Docs" was left out: this install turns off monitors of type ping.'],
    ["monitor_skipped_target_not_allowed", { name: "Db", reason: "10.0.0.9" }, 'The monitor "Db" was left out: this install blocks its target 10.0.0.9.'],
    ["push_token_replaced", { name: "Cron" }, 'The monitor "Cron" got a new push token, since another monitor holds the one in the file. Update the address your job sends to.'],
    ["monitor_paused_over_limit", { name: "API" }, 'The monitor "API" comes in paused: this workspace\'s plan has no room for another running monitor.'],
    ["status_page_slug_changed", { name: "Status", from: "status", to: "status-2" }, 'The status page "Status" has the address status-2 instead of status, since that one is taken.'],
    ["status_page_skipped_over_limit", { name: "Status" }, 'The status page "Status" was left out: this workspace\'s plan has no room for another one.'],
    ["custom_domain_dropped", { name: "status.example.com" }, "The custom domain status.example.com was left out: another status page holds it."],
    ["custom_domain_dropped_over_limit", { name: "status.example.com" }, "The custom domain status.example.com was left out: this workspace's plan has no room for another one."],
    ["retention_capped", { name: "dataRetentionDays", from: "800", to: "365" }, "The data retention period is 365 days instead of 800, the most this workspace's plan allows."],
  ])("words %s", (kind, rest, expected) => {
    expect(changeMessage(change(kind, rest), t)).toBe(expected);
  });

  it("says which interval was raised", () => {
    const raised = { name: "API", from: "5", to: "20" };
    expect(changeMessage(change("monitor_interval_raised", { ...raised, reason: "interval" }), t)).toBe(
      'The monitor "API" checks every 20 seconds instead of 5, the shortest this install allows.'
    );
    expect(changeMessage(change("monitor_interval_raised", { ...raised, reason: "retryInterval" }), t)).toBe(
      'The monitor "API" retries a failed check every 20 seconds instead of 5, the shortest this install allows.'
    );
  });

  it("says why a notification channel was left out, when the reason is one we know", () => {
    const skipped = (reason?: string) => changeMessage(change("channel_skipped", { name: "Ops", ...(reason ? { reason } : {}) }), t);
    expect(skipped("target_not_allowed")).toBe('The notification channel "Ops" was left out: this install blocks the address it sends to.');
    expect(skipped("no_install_sender")).toBe('The notification channel "Ops" was left out: this install has no sender for emails.');
    expect(skipped("recipient_not_member")).toBe('The notification channel "Ops" was left out: its email recipient is not a member of this workspace.');
    expect(skipped("something_new")).toBe('The notification channel "Ops" was left out: this install or workspace can\'t take it.');
    expect(skipped()).toBe('The notification channel "Ops" was left out: this install or workspace can\'t take it.');
  });

  it("gives a kind it doesn't know a generic line", () => {
    expect(changeMessage({ kind: "brand_new_kind", record: "tag", name: "prod" }, t)).toBe(
      "The import changed tag prod (brand_new_kind)."
    );
    expect(changeMessage({ kind: "brand_new_kind", record: "settings" }, t)).toBe(
      "The import changed settings (brand_new_kind)."
    );
  });
});

describe("createdLines", () => {
  it("lists what was created, leaving out what wasn't", () => {
    expect(
      createdLines({ tags: 1, proxies: 0, notificationChannels: 2, monitors: 3, maintenances: 0, statusPages: 1 }, t)
    ).toEqual([
      { key: "tags", label: "Tags", count: 1 },
      { key: "notificationChannels", label: "Notification channels", count: 2 },
      { key: "monitors", label: "Monitors", count: 3 },
      { key: "statusPages", label: "Status pages", count: 1 },
    ]);
  });

  it("is empty when nothing was created", () => {
    expect(createdLines({ tags: 0, proxies: 0, notificationChannels: 0, monitors: 0, maintenances: 0, statusPages: 0 }, t)).toEqual([]);
  });
});

const rejected = (data: unknown, status = 400) => {
  const response = { status, data, statusText: "", headers: {}, config: { headers: new AxiosHeaders() } } as AxiosResponse;
  return new AxiosError("Request failed", "ERR_BAD_REQUEST", undefined, undefined, response);
};

describe("importProblems", () => {
  it("is the problems of a rejected file", () => {
    const problems = [{ record: "monitor", ref: "m1", message: "config is invalid: url is required" }];
    expect(importProblems(rejected({ message: "x", error: { code: "INVALID_WORKSPACE_FILE", problems } }))).toEqual(problems);
  });

  it("is undefined for any other failure", () => {
    expect(importProblems(rejected({ message: "forbidden" }, 403))).toBeUndefined();
    expect(importProblems(rejected({ error: { code: "OTHER", problems: [] } }))).toBeUndefined();
    expect(importProblems(new Error("network"))).toBeUndefined();
  });

  it("is an empty list when the server names none", () => {
    expect(importProblems(rejected({ error: { code: "INVALID_WORKSPACE_FILE", problems: null } }))).toEqual([]);
  });
});

describe("problemLine", () => {
  it("names the record and its ref before the message", () => {
    expect(problemLine({ record: "monitor", ref: "m1", message: "bad" })).toBe("monitor m1: bad");
    expect(problemLine({ record: "file", message: "format must be peekaping-workspace" })).toBe(
      "file: format must be peekaping-workspace"
    );
  });
});

describe("readImportFile", () => {
  const file = (text: string, size = text.length) => ({ size, text: async () => text });

  it("parses a JSON object", async () => {
    expect(await readImportFile(file('{"format":"peekaping-workspace","version":1}'))).toEqual({
      ok: true,
      document: { format: "peekaping-workspace", version: 1 },
    });
  });

  it("refuses text that is not JSON, and JSON that is not an object", async () => {
    expect(await readImportFile(file("{not json"))).toEqual({ ok: false, reason: "not_json" });
    expect(await readImportFile(file(""))).toEqual({ ok: false, reason: "not_json" });
    expect(await readImportFile(file("[1,2]"))).toEqual({ ok: false, reason: "not_json" });
    expect(await readImportFile(file("null"))).toEqual({ ok: false, reason: "not_json" });
    expect(await readImportFile(file('"text"'))).toEqual({ ok: false, reason: "not_json" });
  });

  it("refuses a file over the server's limit without reading it", async () => {
    let read = false;
    const big = { size: MAX_IMPORT_BYTES + 1, text: async () => ((read = true), "{}") };
    expect(await readImportFile(big)).toEqual({ ok: false, reason: "too_large" });
    expect(read).toBe(false);
    expect((await readImportFile(file("{}", MAX_IMPORT_BYTES))).ok).toBe(true);
  });
});

describe("affectedByImport", () => {
  const key = (_id: string) => [{ _id, baseURL: "" }] as const;

  it("is every query of the records an import adds, and the settings it caps", () => {
    for (const id of [
      "getMonitors",
      "getMonitorsById",
      "getNotificationChannels",
      "getTags",
      "getProxies",
      "getMaintenances",
      "getStatusPages",
      "getSettingsKeyByKey",
      // usage and retention, which the import changes
      "getWorkspaceLimits",
    ]) {
      expect(affectedByImport(key(id)), id).toBe(true);
    }
  });

  it("is not the queries about the user", () => {
    expect(affectedByImport(key("getMe"))).toBe(false);
    expect(affectedByImport([])).toBe(false);
    expect(affectedByImport(["getMonitors"])).toBe(false);
  });
});

describe("latestOnly", () => {
  // A read the test finishes by hand
  const deferred = <T,>() => {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((done) => (resolve = done));
    return { promise, resolve };
  };

  it("keeps the second selection when the first read resolves last", async () => {
    const reads = { first: deferred<string>(), second: deferred<string>() };
    const readLatest = latestOnly((name: "first" | "second") => reads[name].promise);

    const first = readLatest("first");
    const second = readLatest("second");
    reads.second.resolve("second document");
    reads.first.resolve("first document");

    expect(await second).toBe("second document");
    expect(await first).toBeUndefined();
  });

  it("applies a read that is still the latest", async () => {
    const readLatest = latestOnly(async (name: string) => name);
    expect(await readLatest("a")).toBe("a");
    expect(await readLatest("b")).toBe("b");
  });
});
