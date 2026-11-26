import { describe, expect, test } from "bun:test";
import { dayRange, escapeHtml, latestCompleteDay, niceMax, renderDashboard } from "./dashboard-html";
import { parseDashboardArgs, run } from "./dashboard";

const NOW = new Date("2026-10-27T15:30:00Z");
const ENV = { CLOUDFLARE_ACCOUNT_ID: "acct123", CLOUDFLARE_ANALYTICS_TOKEN: "tok456" };

describe("dayRange", () => {
  test("lists the UTC days of the range, oldest first, ending today", () => {
    expect(dayRange(3, NOW)).toEqual(["2026-10-25", "2026-10-26", "2026-10-27"]);
    expect(dayRange(1, new Date("2026-10-27T23:59:59Z"))).toEqual(["2026-10-27"]);
  });
});

describe("latestCompleteDay", () => {
  const row = (day: string, instances: string) => ({ day: `${day} 00:00:00`, instances });

  test("picks the latest finished day with pings", () => {
    const rows = [row("2026-10-25", "4"), row("2026-10-26", "5"), row("2026-10-27", "1")];
    expect(latestCompleteDay(rows, "2026-10-27")).toBe("2026-10-26");
  });

  test("falls back to today when only today has pings", () => {
    expect(latestCompleteDay([row("2026-10-27", "2")], "2026-10-27")).toBe("2026-10-27");
  });

  test("is undefined without pings", () => {
    expect(latestCompleteDay([], "2026-10-27")).toBeUndefined();
  });
});

describe("niceMax", () => {
  test.each([
    [0, 1],
    [1, 1],
    [3, 5],
    [7, 10],
    [12, 20],
    [51, 100],
    [200, 200],
  ])("rounds %p up to %p", (value, max) => {
    expect(niceMax(value)).toBe(max);
  });
});

describe("renderDashboard", () => {
  const data = {
    days: 3,
    generatedAt: NOW,
    overall: [
      { day: "2026-10-25 00:00:00", instances: "4" },
      { day: "2026-10-26 00:00:00", instances: "5" },
      { day: "2026-10-27 00:00:00", instances: "2" },
    ],
    breakdowns: [
      {
        title: "Distinct instances per day by version",
        label: "version",
        rows: [
          { day: "2026-10-26 00:00:00", value: "0.0.48", instances: "3" },
          { day: "2026-10-26 00:00:00", value: "0.0.49", instances: "2" },
          { day: "2026-10-27 00:00:00", value: "0.0.50", instances: "2" },
        ],
      },
    ],
  };

  test("shows the latest finished day in the tiles and the breakdowns", () => {
    const html = renderDashboard(data);
    expect(html).toContain('<div class="tile-value">5</div><div class="tile-note">on Oct 26</div>');
    expect(html).toContain('<div class="tile-value">2</div><div class="tile-note">UTC, still counting</div>');
    expect(html).toContain("<h2>Version</h2>");
    expect(html).toContain('title="0.0.48: 3 installs, 60%"');
    expect(html).toContain('title="0.0.49: 2 installs, 40%"');
    // Today's partial breakdown stays out of the bars, but is in the tables
    expect(html).not.toContain('title="0.0.50');
    expect(html).toContain("<td>0.0.50</td>");
  });

  test("charts the finished days only", () => {
    const html = renderDashboard(data);
    const points = JSON.parse(html.match(/data-points='([^']*)'/)![1]!.replaceAll("&quot;", '"'));
    expect(points).toEqual([
      { day: "2026-10-25", instances: 4 },
      { day: "2026-10-26", instances: 5 },
    ]);
  });

  test("charts today alone when the range is one day", () => {
    const html = renderDashboard({ ...data, days: 1 });
    expect(html).toContain('data-points=\'[{&quot;day&quot;:&quot;2026-10-27&quot;,&quot;instances&quot;:2}]\'');
  });

  test("fills days without pings with zero", () => {
    const html = renderDashboard({ ...data, days: 5 });
    expect(html).toContain("<td>2026-10-23</td><td>0</td>");
  });

  test("renders an empty dataset", () => {
    const html = renderDashboard({ ...data, overall: [], breakdowns: [{ ...data.breakdowns[0]!, rows: [] }] });
    expect(html).toContain("No pings in this range yet.");
    expect(html).toContain("No data for this day.");
  });

  test("loads nothing from the network", () => {
    expect(renderDashboard(data)).not.toMatch(/<(script|link)[^>]+(src|href)=/);
  });

  test("escapes values from the dataset", () => {
    const hostile = { ...data.breakdowns[0]!, rows: [{ day: "2026-10-26 00:00:00", value: "<img onerror=x>", instances: "1" }] };
    const html = renderDashboard({ ...data, breakdowns: [hostile] });
    expect(html).not.toContain("<img");
    expect(html).toContain(escapeHtml("<img onerror=x>"));
  });
});

describe("parseDashboardArgs", () => {
  test("defaults", () => {
    expect(parseDashboardArgs([])).toEqual({ days: 30, out: "dashboard.html", open: true });
  });

  test("reads --days, --out and --no-open", () => {
    expect(parseDashboardArgs(["--days", "7", "--out", "x.html", "--no-open"])).toEqual({
      days: 7,
      out: "x.html",
      open: false,
    });
  });

  test("refuses a bad --days and unknown flags", () => {
    expect(() => parseDashboardArgs(["--days", "0"])).toThrow();
    expect(() => parseDashboardArgs(["--verbose"])).toThrow();
  });
});

describe("run", () => {
  function setup(env: Record<string, string | undefined> = ENV) {
    const bodies: string[] = [];
    const written: { path: string; content: string }[] = [];
    const opened: string[] = [];
    const fetchFn = (async (_url: string | URL | Request, init?: RequestInit) => {
      bodies.push(String(init?.body));
      return new Response(JSON.stringify({ data: [{ day: "2026-10-26 00:00:00", value: "http", instances: "3" }] }));
    }) as typeof fetch;
    const options = {
      argv: ["--out", "/tmp/out.html"],
      env,
      fetch: fetchFn,
      now: () => NOW,
      writeFile: async (path: string, content: string) => void written.push({ path, content }),
      open: (path: string) => void opened.push(path),
      print: () => {},
    };
    return { options, bodies, written, opened };
  }

  test("runs the six queries, writes the page and opens it", async () => {
    const { options, bodies, written, opened } = setup();
    await run(options);
    expect(bodies).toHaveLength(6);
    expect(written).toHaveLength(1);
    expect(written[0]!.path).toBe("/tmp/out.html");
    expect(written[0]!.content).toContain("<title>Peekaping telemetry</title>");
    expect(written[0]!.content).not.toContain("tok456");
    expect(opened).toEqual(["/tmp/out.html"]);
  });

  test("does not open with --no-open", async () => {
    const { options, opened } = setup();
    await run({ ...options, argv: [...options.argv, "--no-open"] });
    expect(opened).toEqual([]);
  });

  test("stops without credentials, calling and writing nothing", async () => {
    const { options, bodies, written } = setup({ ...ENV, CLOUDFLARE_ANALYTICS_TOKEN: undefined });
    await expect(run(options)).rejects.toThrow("CLOUDFLARE_ANALYTICS_TOKEN");
    expect(bodies).toHaveLength(0);
    expect(written).toHaveLength(0);
  });
});
