import { describe, expect, test } from "bun:test";
import { run } from "./report";

const ENV = { CLOUDFLARE_ACCOUNT_ID: "acct123", CLOUDFLARE_ANALYTICS_TOKEN: "tok456" };

interface Call {
  url: string;
  init: RequestInit;
}

// A stand-in for the Analytics Engine SQL API: it records calls and answers every query the same way.
function fakeApi(answer: () => Response) {
  const calls: Call[] = [];
  const fetchFn = async (url: string | URL | Request, init?: RequestInit): Promise<Response> => {
    calls.push({ url: String(url), init: init ?? {} });
    return answer();
  };
  return { calls, fetchFn: fetchFn as typeof fetch };
}

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
}

async function runWith(argv: string[], env: Record<string, string | undefined>, fetchFn: typeof fetch) {
  const lines: string[] = [];
  await run({ argv, env, fetch: fetchFn, print: (text) => lines.push(text) });
  return lines.join("\n");
}

describe("run", () => {
  test("posts the six reports to the SQL API and prints their tables", async () => {
    const api = fakeApi(() =>
      json({
        meta: [],
        data: [{ day: "2026-10-21 00:00:00", value: "0.0.48", instances: "7" }],
        rows: 1,
      }),
    );

    const output = await runWith([], ENV, api.fetchFn);

    expect(api.calls).toHaveLength(6);
    for (const call of api.calls) {
      expect(call.url).toBe("https://api.cloudflare.com/client/v4/accounts/acct123/analytics_engine/sql");
      expect(call.init.method).toBe("POST");
      expect(call.init.headers).toEqual({ Authorization: "Bearer tok456" });
    }
    expect(String(api.calls[0]!.init.body)).toContain("FROM peekaping_telemetry WHERE blob1 = 'ping'");
    expect(String(api.calls[0]!.init.body)).toContain("INTERVAL '29' DAY");
    expect(output).toContain("Distinct instances per day by version\nday         version  instances");
    expect(output).toContain("2026-10-21  0.0.48          7");
    expect(output.indexOf("by version")).toBeLessThan(output.indexOf("by notification channel type"));
  });

  test("reads --days", async () => {
    const api = fakeApi(() => json({ data: [] }));
    const output = await runWith(["--days", "7"], ENV, api.fetchFn);
    expect(String(api.calls[0]!.init.body)).toContain("INTERVAL '6' DAY");
    expect(output).toContain("(no data)");
  });

  test.each(["CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_ANALYTICS_TOKEN"])("stops without %s, calling nothing", async (name) => {
    const api = fakeApi(() => json({ data: [] }));
    const env = { ...ENV, [name]: undefined };
    await expect(runWith([], env, api.fetchFn)).rejects.toThrow(name);
    expect(api.calls).toHaveLength(0);
  });

  test("reports the status and the API's message when a query fails, without the token", async () => {
    const api = fakeApi(() => new Response("unable to parse query", { status: 400 }));
    const failure = runWith([], ENV, api.fetchFn);
    await expect(failure).rejects.toThrow("400");
    await expect(failure).rejects.toThrow("unable to parse query");
    await expect(failure.catch((error: Error) => error.message)).resolves.not.toContain("tok456");
  });

  test("stops on an answer that has no data rows", async () => {
    const api = fakeApi(() => json({ errors: [] }));
    await expect(runWith([], ENV, api.fetchFn)).rejects.toThrow("data");
  });
});
