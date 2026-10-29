import { describe, expect, it, vi } from "vitest";
import {
  GET_STARTED_PATH,
  createChannelAndLink,
  firstMonitorBody,
  getStartedChannelTypes,
  getStartedGate,
  linkChannel,
  nextStepLinks,
  parseMonitorUrl,
} from "./get-started";
import { channelCreateBody } from "@/app/notification-channels/channel-payload";

describe("parseMonitorUrl", () => {
  it.each([
    ["https://example.com", "example.com"],
    ["  http://status.example.org/health?x=1  ", "status.example.org"],
    ["https://example.com:8443/", "example.com:8443"],
  ])("accepts %j, named after the host %j", (input, host) => {
    const parsed = parseMonitorUrl(input);
    expect(parsed).toEqual({ url: input.trim(), host });
  });

  it.each(["", "example.com", "ftp://example.com", "javascript:alert(1)", "https://", "not a url", "tcp://example.com:25"])(
    "refuses %j",
    (input) => {
      expect(parseMonitorUrl(input)).toBeNull();
    }
  );
});

describe("firstMonitorBody", () => {
  const body = firstMonitorBody({ url: "https://example.com/health", host: "example.com" });
  // What the request carries on the wire
  const sent = JSON.parse(JSON.stringify(body));

  it("is an HTTP monitor named after the host, aimed at the URL", () => {
    expect(sent.type).toBe("http");
    expect(sent.name).toBe("example.com");
    expect(JSON.parse(sent.config).url).toBe("https://example.com/health");
  });

  it("names no probe, so that the monitor runs on the install's default probes", () => {
    expect(sent).not.toHaveProperty("probe_ids");
  });

  it("has the full form's defaults for everything else", () => {
    expect(sent).toMatchObject({
      interval: 20,
      timeout: 16,
      max_retries: 0,
      retry_interval: 20,
      resend_interval: 0,
      notification_ids: [],
      tag_ids: [],
      location_rule: "any",
      proxy_id: "",
    });
    expect(JSON.parse(sent.config)).toMatchObject({
      method: "GET",
      accepted_statuscodes: ["2XX"],
      max_redirects: 10,
      ignore_tls_errors: false,
      check_cert_expiry: false,
      authMethod: "none",
    });
  });
});

describe("getStartedChannelTypes", () => {
  it("offers Email first, then Telegram, Slack, Discord and webhook, when the install has a sender", () => {
    expect(getStartedChannelTypes(true)).toEqual(["install_email", "telegram", "slack", "discord", "webhook"]);
  });

  it("offers only the four others without a sender", () => {
    expect(getStartedChannelTypes(false)).toEqual(["telegram", "slack", "discord", "webhook"]);
  });
});

describe("channelCreateBody", () => {
  it("is the config as JSON, active, and the default for new monitors when asked", () => {
    const body = channelCreateBody({ name: "Me", type: "install_email", to: "a@example.com" }, true);
    expect(body).toMatchObject({ name: "Me", type: "install_email", active: true, is_default: true });
    expect(JSON.parse(body.config as string)).toMatchObject({ to: "a@example.com" });
  });

  it("is not a default otherwise", () => {
    expect(channelCreateBody({ name: "Me", type: "install_email", to: "a@example.com" }, false).is_default).toBe(false);
  });
});

describe("createChannelAndLink", () => {
  const values = { name: "Alerts", type: "slack" as const, webhook_url: "https://hooks.example/x" } as never;

  it("creates the channel as the default, then links it to the monitor the step before made", async () => {
    const calls: string[] = [];
    const create = vi.fn(async () => {
      calls.push("create");
      return { id: "c1" };
    });
    const link = vi.fn(async () => void calls.push("link"));

    const result = await createChannelAndLink({ values, monitorId: "m1", create, link });

    expect(result).toEqual({ kind: "linked", channelId: "c1" });
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ name: "Alerts", type: "slack", is_default: true }));
    expect(link).toHaveBeenCalledWith("m1", "c1");
    expect(calls).toEqual(["create", "link"]);
  });

  it("only creates it when no monitor was made", async () => {
    const create = vi.fn().mockResolvedValue({ id: "c1" });
    const link = vi.fn();

    await expect(createChannelAndLink({ values, monitorId: null, create, link })).resolves.toEqual({
      kind: "created",
      channelId: "c1",
    });
    expect(link).not.toHaveBeenCalled();
  });

  it("throws when the channel was not created, and links nothing", async () => {
    const failure = new Error("400");
    const create = vi.fn().mockRejectedValue(failure);
    const link = vi.fn();

    await expect(createChannelAndLink({ values, monitorId: "m1", create, link })).rejects.toBe(failure);
    expect(link).not.toHaveBeenCalled();
  });

  it("reports a failed link apart, since the channel exists by then", async () => {
    const create = vi.fn().mockResolvedValue({ id: "c1" });
    const link = vi.fn().mockRejectedValue(new Error("500"));
    const log = vi.spyOn(console, "error").mockImplementation(() => {});

    await expect(createChannelAndLink({ values, monitorId: "m1", create, link })).resolves.toEqual({
      kind: "link_failed",
      channelId: "c1",
    });
    log.mockRestore();
  });
});

// Step 3's "Try linking again", after a save whose link failed
describe("linkChannel", () => {
  it("links the channel already made to the monitor", async () => {
    const link = vi.fn().mockResolvedValue(undefined);

    await expect(linkChannel({ monitorId: "m1", channelId: "c1", link })).resolves.toEqual({
      kind: "linked",
      channelId: "c1",
    });
    expect(link).toHaveBeenCalledTimes(1);
    expect(link).toHaveBeenCalledWith("m1", "c1");
  });

  it("reports a failed link again, keeping the channel", async () => {
    const link = vi.fn().mockRejectedValue(new Error("500"));
    const log = vi.spyOn(console, "error").mockImplementation(() => {});

    await expect(linkChannel({ monitorId: "m1", channelId: "c1", link })).resolves.toEqual({
      kind: "link_failed",
      channelId: "c1",
    });
    log.mockRestore();
  });
});

describe("getStartedGate", () => {
  it("waits for the workspace's monitors", () => {
    expect(getStartedGate({ pending: true, error: false, monitors: undefined })).toBe("loading");
  });

  it("shows Get started on a workspace without monitors", () => {
    expect(getStartedGate({ pending: false, error: false, monitors: [] })).toBe("show");
  });

  it("shows Get started when the API answers the empty list as null", () => {
    expect(getStartedGate({ pending: false, error: false, monitors: null })).toBe("show");
  });

  it("sends a workspace that has a monitor to the monitors list", () => {
    expect(getStartedGate({ pending: false, error: false, monitors: [{}] })).toBe("monitors");
  });

  it("sends the user to the monitors list when it can't tell", () => {
    expect(getStartedGate({ pending: false, error: true, monitors: undefined })).toBe("monitors");
  });
});

describe("nextStepLinks", () => {
  it("opens the new monitor, and leads to status pages and members", () => {
    expect(nextStepLinks({ monitorId: "m1", instanceAdmin: false })).toEqual([
      { key: "monitor", path: "/monitors/m1" },
      { key: "status_pages", path: "/status-pages" },
      { key: "members", path: "/settings/members" },
    ]);
  });

  it("goes to the monitors list when no monitor was made", () => {
    expect(nextStepLinks({ monitorId: null, instanceAdmin: false })[0]).toEqual({ key: "monitors", path: "/monitors" });
  });

  it("adds the admin area's probes, outside the workspace, for an instance admin", () => {
    expect(nextStepLinks({ monitorId: "m1", instanceAdmin: true }).at(-1)).toEqual({
      key: "probes",
      path: "/admin/probes",
      outsideWorkspace: true,
    });
  });
});

it("opens Get started at /get-started", () => {
  expect(GET_STARTED_PATH).toBe("/get-started");
});
