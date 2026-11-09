import { describe, expect, test } from "bun:test";
import { dayRange, escapeHtml, latestCompleteDay, niceMax, renderDashboard } from "./dashboard-html";
import { parseDashboardArgs, run } from "./dashboard";

const NOW = new Date("2026-10-27T15:30:00Z");

// A summary row as the SQL API returns it. Today is Tue Oct 27, so the weekly window is Oct 20 to Oct 26.
function install(first: string, last: string, extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    first_day: `${first} 00:00:00`,
    last_day: `${last} 00:00:00`,
    in_week: 0,
    in_month: 0,
    monitors: "1-10",
    status_pages: "false",
    maintenance: "false",
    metrics: "false",
    ...extra,
  };
}
const ACTIVE = { in_week: 1, in_month: 1 };

// Six installs, all active in the week; the data starts on Oct 20
const INSTALLS = [
  install("2026-10-20", "2026-10-26", { ...ACTIVE, status_pages: "true" }),
  install("2026-10-20", "2026-10-26", { ...ACTIVE, monitors: "0" }),
  install("2026-10-20", "2026-10-26", { ...ACTIVE, monitors: "11-50", status_pages: "true", maintenance: "true" }),
  install("2026-10-21", "2026-10-26", { ...ACTIVE, metrics: "true" }),
  install("2026-10-22", "2026-10-26", { ...ACTIVE, monitors: "51-200", status_pages: "true" }),
  install("2026-10-20", "2026-10-20", ACTIVE),
];
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
    installs: INSTALLS,
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

describe("renderDashboard install metrics", () => {
  const data = {
    days: 3,
    generatedAt: NOW,
    installs: INSTALLS,
    overall: [
      { day: "2026-10-25 00:00:00", instances: "4" },
      { day: "2026-10-26 00:00:00", instances: "5" },
      { day: "2026-10-27 00:00:00", instances: "2" },
    ],
    breakdowns: [],
  };
  const html = renderDashboard(data);

  test("leads with the weekly active installs as the headline tile", () => {
    expect(html).toContain(
      '<div class="tile headline"><div class="tile-label">Weekly active installs</div>' +
        '<div class="tile-value">6</div><div class="tile-note">last 7 finished days</div>',
    );
    const order = [
      "Weekly active installs",
      "Daily active installs",
      "Monthly active installs",
      "Stickiness",
      "Today so far",
      "Peak, last 3 days",
    ].map((label) => html.indexOf(`<div class="tile-label">${label}</div>`));
    expect(order.every((at) => at > 0)).toBe(true);
    expect(order).toEqual([...order].sort((a, b) => a - b));
  });

  test("shows daily, monthly and stickiness next to it", () => {
    expect(html).toContain('<div class="tile-value">5</div><div class="tile-note">on Oct 26</div>');
    expect(html).toContain('<div class="tile-value">6</div><div class="tile-note">data covers only 7 finished days</div>');
    // 5 daily over 6 monthly
    expect(html).toContain('<div class="tile-value">83%</div><div class="tile-note">daily ÷ monthly active</div>');
  });

  test("says so when the data covers fewer days than the window", () => {
    const week = renderDashboard({ ...data, installs: [install("2026-10-24", "2026-10-26", ACTIVE)] });
    expect(week).toContain("data covers only 3 finished days");
    expect(week).not.toContain("last 7 finished days");
    const day = renderDashboard({ ...data, installs: [install("2026-10-26", "2026-10-26", ACTIVE)] });
    expect(day).toContain("data covers only 1 finished day<");
  });

  test("gives every tile a one-line explanation", () => {
    expect(html.match(/<div class="tile( headline)?">/g)).toHaveLength(6);
    expect(html.match(/<div class="tile-why">/g)).toHaveLength(6);
    expect(html).toContain("Installs with a usage ping in the last 7 finished UTC days.");
  });

  test("shows activation and feature adoption as bars of the weekly active installs", () => {
    expect(html).toContain('title="More than 0 monitors: 5 installs, 83%"');
    expect(html).toContain('title="Status pages: 3 installs, 50%"');
    expect(html).toContain('title="Maintenance windows: 1 installs, 17%"');
    expect(html).toContain('title="Metrics endpoint: 1 installs, 17%"');
  });

  test("explains each new section in a line under its heading", () => {
    for (const [id, heading] of [
      ["growth", "New and churned installs per week"],
      ["cohorts", "Retention of weekly cohorts"],
      ["activation", "Activation"],
      ["adoption", "Feature adoption"],
      ["limits", "Limits"],
    ]) {
      expect(html).toContain(`<section class="card" id="${id}"><h2>${heading}</h2><p class="why">`);
    }
  });

  test("lists the limits of the data", () => {
    const limits = html.slice(html.indexOf('id="limits"'));
    expect(limits).toContain("TELEMETRY=off");
    expect(limits).toContain("opt out");
    expect(limits).toContain("instance ID changes");
    expect(limits).toContain("one install can count as churned and then as new");
    expect(limits).toContain("replicas share one instance ID");
    expect(limits).toContain("kept for 92 days");
    expect(limits).toContain("daily count reads a few percent low");
    expect(limits).toContain("comes back after 7 or more silent days is not counted as churned");
  });

  // Data from Sep 30: weeks start Sep 28, Oct 5, Oct 12, Oct 19, Oct 26 (so far)
  const history = renderDashboard({
    ...data,
    installs: [
      install("2026-09-30", "2026-10-26", ACTIVE),
      install("2026-10-05", "2026-10-05"),
      install("2026-10-06", "2026-10-07"),
      install("2026-10-11", "2026-10-18"),
      install("2026-10-08", "2026-10-14"),
    ],
  });

  test("tabulates new, churned and net installs per week, with the current week so far", () => {
    // First week of the data: no new or net numbers
    expect(history).toContain("<td>Sep 28</td><td>–</td><td>0</td><td>–</td>");
    // Cohort of 4 first seen Oct 5 week; two of them have their 7th silent day on Oct 12 and Oct 14
    expect(history).toContain("<td>Oct 5</td><td>4</td><td>0</td><td>+4</td>");
    expect(history).toContain("<td>Oct 12</td><td>0</td><td>2</td><td>-2</td>");
    expect(history).toContain("<td>Oct 26 (so far)</td><td>0</td><td>0</td><td>0</td>");
    expect(history).toContain("<th>churned (estimate)</th>");
  });

  test("charts the weeks as inline SVG bars in two colors, churned marked as an estimate", () => {
    expect(history).toContain("<title>Oct 12: 2 churned (estimate)</title>");
    expect(history).toContain("<title>Oct 5: 4 new</title>");
    expect(history).toContain("Churned (estimate)");
    expect(history).toContain('class="bar-new"');
    expect(history).toContain('class="bar-churned"');
  });

  test("tabulates cohort retention with counts, leaving unmeasurable cells blank", () => {
    expect(history).toContain("<td>Oct 5</td><td>4</td><td>75% (3 of 4)</td><td>25% (1 of 4)</td><td></td>");
    // The current week's cohort is empty and nothing in it is measurable
    expect(history).toContain("<td>Oct 26</td><td>0</td><td></td><td></td><td></td>");
    // The week the data starts in is left out
    expect(history).not.toContain("<td>Sep 28</td><td>0</td>");
  });

  test("renders an empty dataset", () => {
    const empty = renderDashboard({ ...data, installs: [], overall: [] });
    expect(empty).toContain("No weekly active installs yet.");
    expect(empty).toContain('<div class="tile-value">–</div><div class="tile-note">daily ÷ monthly active</div>');
  });

  test("defines the new colors for the light and the dark theme", () => {
    const [light, dark] = html.match(/:root \{[^}]*\}/g)!;
    for (const token of ["--new:", "--churned:"]) {
      expect(light).toContain(token);
      expect(dark).toContain(token);
    }
    expect(html.indexOf("prefers-color-scheme: dark")).toBeLessThan(html.indexOf(dark!));
  });

  test("stays self-contained: loads nothing and holds no instance ID", () => {
    const hostile = [{ ...INSTALLS[0]!, index1: "0b8f6c1e-5d3a-4b7e-9c2a-1f4e8d7a6b50" }];
    const page = renderDashboard({ ...data, installs: hostile });
    expect(page).not.toMatch(/\b(src|href)=/);
    expect(page).not.toMatch(/https?:\/\//);
    expect(page).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i);
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
      const sql = String(init?.body);
      bodies.push(sql);
      const rows = sql.includes("GROUP BY index1")
        ? [install("2026-10-20", "2026-10-26", ACTIVE), install("2026-10-22", "2026-10-26", ACTIVE)]
        : [{ day: "2026-10-26 00:00:00", value: "http", instances: "3" }];
      return new Response(JSON.stringify({ data: rows }));
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

  test("runs the seven queries, writes the page and opens it", async () => {
    const { options, bodies, written, opened } = setup();
    await run(options);
    expect(bodies).toHaveLength(7);
    expect(written).toHaveLength(1);
    expect(written[0]!.path).toBe("/tmp/out.html");
    expect(written[0]!.content).toContain("<title>Peekaping telemetry</title>");
    expect(written[0]!.content).not.toContain("tok456");
    // The two installs from the summary query reach the headline tile
    expect(written[0]!.content).toContain('<div class="tile-label">Weekly active installs</div><div class="tile-value">2</div>');
    expect(opened).toEqual(["/tmp/out.html"]);
  });

  test("sends the install summary last, over the 92 retained days whatever --days is", async () => {
    const { options, bodies } = setup();
    await run({ ...options, argv: [...options.argv, "--days", "7"] });
    expect(bodies).toHaveLength(7);
    expect(bodies[0]).toContain("INTERVAL '6' DAY");
    expect(bodies[6]).toContain("GROUP BY index1");
    expect(bodies[6]).toContain("INTERVAL '91' DAY");
    expect(bodies.filter((sql) => sql.includes("GROUP BY index1"))).toHaveLength(1);
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
