import { beforeEach, describe, expect, test } from "bun:test";
import { type Env, handleRequest } from "./handler";

const ENDPOINT = "https://telemetry.peekaping.com/v1/ping";
const INSTANCE_ID = "0b8f6c1e-5d3a-4b7e-9c2a-1f4e8d7a6b50";

// The payload contract from ticket #232.
function example(): Record<string, unknown> {
  return {
    instance_id: INSTANCE_ID,
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

// The stand-in for the Analytics Engine binding: it keeps what the Worker writes.
class FakeDataset implements AnalyticsEngineDataset {
  points: AnalyticsEngineDataPoint[] = [];
  writeDataPoint(point?: AnalyticsEngineDataPoint): void {
    if (point) this.points.push(point);
  }
}

let dataset: FakeDataset;
let env: Env;

beforeEach(() => {
  dataset = new FakeDataset();
  env = { TELEMETRY: dataset };
});

function post(body: BodyInit | null, init: RequestInit = {}): Request {
  return new Request(ENDPOINT, { method: "POST", body, ...init });
}

function postJson(payload: unknown, init: RequestInit = {}): Request {
  return post(JSON.stringify(payload), init);
}

describe("POST /v1/ping with a valid ping", () => {
  test("answers 204 with no body and writes one point for the ping and one for each type", async () => {
    const response = await handleRequest(postJson(example()), env);

    expect(response.status).toBe(204);
    expect(await response.text()).toBe("");
    expect(dataset.points).toEqual([
      {
        indexes: [INSTANCE_ID],
        blobs: ["ping", "0.0.48", "single", "sqlite", "linux/arm64", "11-50", "true", "false", "true"],
      },
      { indexes: [INSTANCE_ID], blobs: ["monitor_type", "http"] },
      { indexes: [INSTANCE_ID], blobs: ["monitor_type", "ping"] },
      { indexes: [INSTANCE_ID], blobs: ["monitor_type", "docker"] },
      { indexes: [INSTANCE_ID], blobs: ["notification_provider", "telegram"] },
      { indexes: [INSTANCE_ID], blobs: ["notification_provider", "slack"] },
    ]);
  });

  test("writes only the ping point when no monitor type or provider is configured", async () => {
    const payload = { ...example(), monitor_types: [], notification_providers: [] };
    const response = await handleRequest(postJson(payload), env);

    expect(response.status).toBe(204);
    expect(dataset.points).toEqual([
      {
        indexes: [INSTANCE_ID],
        blobs: ["ping", "0.0.48", "single", "sqlite", "linux/arm64", "11-50", "true", "false", "true"],
      },
    ]);
  });

  test("stores nothing from the request but the payload: no IP, header, country or cf data", async () => {
    const request = postJson(example(), {
      headers: {
        "CF-Connecting-IP": "203.0.113.9",
        "X-Forwarded-For": "198.51.100.4",
        "CF-IPCountry": "UA",
        "User-Agent": "peekaping/0.0.48",
        "Content-Type": "application/json",
      },
      cf: { country: "UA", city: "Kyiv", asn: 64500, colo: "WAW" },
    } as RequestInit);
    const response = await handleRequest(request, env);

    expect(response.status).toBe(204);
    const stored = JSON.stringify(dataset.points);
    for (const leak of ["203.0.113.9", "198.51.100.4", "UA", "Kyiv", "64500", "WAW", "peekaping/0.0.48"]) {
      expect(stored).not.toContain(leak);
    }
    expect(dataset.points).toHaveLength(6);
    for (const point of dataset.points) {
      expect(Object.keys(point).sort()).toEqual(["blobs", "indexes"]);
    }
  });

  test("fits the Analytics Engine limits in the largest ping: 250 points per invocation, 20 blobs and 16 KB per point, 96 bytes per index", async () => {
    const fifty = Array.from({ length: 50 }, (_, i) => `${"t".repeat(30)}${String(i).padStart(2, "0")}`);
    const payload = {
      ...example(),
      version: `1.2.3-${"a".repeat(26)}`,
      os: `${"a".repeat(15)}/${"b".repeat(16)}`,
      monitor_types: fifty,
      notification_providers: fifty,
    };
    const response = await handleRequest(postJson(payload), env);

    expect(response.status).toBe(204);
    expect(dataset.points).toHaveLength(101);
    expect(dataset.points.length).toBeLessThanOrEqual(250);
    const encoder = new TextEncoder();
    for (const point of dataset.points) {
      expect(point.indexes).toHaveLength(1);
      expect(encoder.encode(String(point.indexes?.[0])).length).toBeLessThanOrEqual(96);
      expect(point.blobs?.length ?? 0).toBeLessThanOrEqual(20);
      const blobBytes = (point.blobs ?? []).reduce((sum, blob) => sum + encoder.encode(String(blob)).length, 0);
      expect(blobBytes).toBeLessThanOrEqual(16 * 1024);
    }
  });

  test("accepts a body of exactly 4096 bytes", async () => {
    const json = JSON.stringify(example());
    const body = json + " ".repeat(4096 - json.length);
    expect(new TextEncoder().encode(body).length).toBe(4096);

    const response = await handleRequest(post(body), env);

    expect(response.status).toBe(204);
    expect(dataset.points).toHaveLength(6);
  });

  test("accepts a query string, which it ignores", async () => {
    const response = await handleRequest(
      new Request(`${ENDPOINT}?utm=x`, { method: "POST", body: JSON.stringify(example()) }),
      env,
    );
    expect(response.status).toBe(204);
  });
});

describe("any other path", () => {
  test.each(["/", "/v1", "/v1/", "/v1/ping/", "/v1/ping/extra", "/v1/pings", "/V1/ping", "/v2/ping", "/ping", "/v1//ping"])(
    "%s answers 404 and stores nothing",
    async (path) => {
      const response = await handleRequest(
        new Request(`https://telemetry.peekaping.com${path}`, { method: "POST", body: JSON.stringify(example()) }),
        env,
      );
      expect(response.status).toBe(404);
      expect(dataset.points).toEqual([]);
    },
  );

  test("answers 404 before it looks at the method", async () => {
    const response = await handleRequest(new Request("https://telemetry.peekaping.com/", { method: "GET" }), env);
    expect(response.status).toBe(404);
  });
});

describe("any other method on /v1/ping", () => {
  test.each(["GET", "HEAD", "PUT", "PATCH", "DELETE", "OPTIONS"])("%s answers 405 and stores nothing", async (method) => {
    const response = await handleRequest(new Request(ENDPOINT, { method }), env);
    expect(response.status).toBe(405);
    expect(response.headers.get("allow")).toBe("POST");
    expect(dataset.points).toEqual([]);
  });

  test("a PUT with a valid body is still not stored", async () => {
    const response = await handleRequest(new Request(ENDPOINT, { method: "PUT", body: JSON.stringify(example()) }), env);
    expect(response.status).toBe(405);
    expect(dataset.points).toEqual([]);
  });
});

describe("a body that is not a valid ping", () => {
  test("above 4096 bytes answers 400 and stores nothing", async () => {
    const json = JSON.stringify(example());
    const response = await handleRequest(post(json + " ".repeat(4097 - json.length)), env);
    expect(response.status).toBe(400);
    expect(dataset.points).toEqual([]);
  });

  test("counts bytes, not characters", async () => {
    // 2100 two-byte characters are 4200 bytes but only 2100 characters.
    const response = await handleRequest(post(JSON.stringify({ ...example(), version: "é".repeat(2100) })), env);
    expect(response.status).toBe(400);
    expect(dataset.points).toEqual([]);
  });

  test("with a Content-Length above 4096 answers 400 without storing", async () => {
    const response = await handleRequest(postJson(example(), { headers: { "Content-Length": "5000" } }), env);
    expect(response.status).toBe(400);
    expect(dataset.points).toEqual([]);
  });

  test("streamed in chunks past 4096 bytes, with no Content-Length, answers 400", async () => {
    const chunk = new TextEncoder().encode(" ".repeat(1024));
    let sent = 0;
    const stream = new ReadableStream<Uint8Array>({
      pull(controller) {
        if (sent === 8) return controller.close();
        sent += 1;
        controller.enqueue(chunk);
      },
    });
    const response = await handleRequest(
      new Request(ENDPOINT, { method: "POST", body: stream, duplex: "half" } as RequestInit),
      env,
    );
    expect(response.status).toBe(400);
    expect(dataset.points).toEqual([]);
  });

  test.each([
    ["no body", null],
    ["an empty body", ""],
    ["text", "not json"],
    ["a truncated object", '{"instance_id":'],
    ["null", "null"],
    ["an array", JSON.stringify([example()])],
    ["a string", '"ping"'],
  ])("%s answers 400", async (_name, body) => {
    const response = await handleRequest(post(body), env);
    expect(response.status).toBe(400);
    expect(dataset.points).toEqual([]);
  });

  test("with bytes that are not UTF-8 answers 400", async () => {
    const valid = new TextEncoder().encode(JSON.stringify(example()));
    const bad = new Uint8Array([...valid.slice(0, -1), 0xff, 0x7d]);
    const response = await handleRequest(post(bad), env);
    expect(response.status).toBe(400);
    expect(dataset.points).toEqual([]);
  });

  test.each([
    ["an extra field", { ...example(), country: "UA" }],
    ["an extra feature", { ...example(), features: { status_pages: true, maintenance: false, metrics: true, x: true } }],
    ["a missing field", { ...example(), os: undefined }],
    ["a bad version", { ...example(), version: "latest" }],
    ["a bad mode", { ...example(), mode: "multi" }],
    ["a bad storage backend", { ...example(), db: "mysql" }],
    ["a bad monitors bucket", { ...example(), monitors: "12" }],
    ["a duplicate monitor type", { ...example(), monitor_types: ["http", "http"] }],
    ["an upper-case instance ID", { ...example(), instance_id: INSTANCE_ID.toUpperCase() }],
  ])("with %s answers 400 and stores nothing", async (_name, payload) => {
    const response = await handleRequest(postJson(payload), env);
    expect(response.status).toBe(400);
    expect(dataset.points).toEqual([]);
  });

  test("answers with a short fixed body that does not echo the input", async () => {
    const secret = "203.0.113.7-secret";
    const bodies = [
      JSON.stringify({ ...example(), [secret]: 1 }),
      JSON.stringify({ ...example(), version: secret }),
      `${secret} is not json`,
    ];
    for (const body of bodies) {
      const response = await handleRequest(post(body), env);
      const text = await response.text();
      expect(response.status).toBe(400);
      expect(text.length).toBeLessThan(80);
      expect(text).not.toContain(secret);
    }
  });
});
