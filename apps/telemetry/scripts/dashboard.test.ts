import { describe, expect, test } from "bun:test";
import { dayRange, escapeHtml, latestCompleteDay, niceMax, renderDashboard } from "./dashboard-html";
import { parseDashboardArgs, run } from "./dashboard";

const NOW = new Date("2026-10-27T15:30:00Z");

// A summary row as the SQL API returns it. Today is Tue Oct 27, so the weekly window is Oct 20 to Oct 26.
function install(first: string, last: string, extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    first_day: `${first} 00:00:00`,
    last_day: `${last} 00:00:00`,
    in_day: 0,
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

// Rows of the active-days query for the 7, 30 and 90 day windows: installs by the number of days with a usage ping
const ACTIVE_DAYS = [
  [
    { days: "1", installs: "1" },
    { days: "3", installs: "1" },
    { days: "6", installs: "2" },
    { days: "7", installs: "2" },
  ],
  [
    { days: "26", installs: "3" },
    { days: "30", installs: "1" },
  ],
  [{ days: "78", installs: "2" }],
];

// Six installs, all active in the week, five of them on the last finished day (Oct 26); the data starts on Oct 20
const DAY = { ...ACTIVE, in_day: 1 };
const INSTALLS = [
  install("2026-10-20", "2026-10-26", { ...DAY, status_pages: "true" }),
  install("2026-10-20", "2026-10-26", { ...DAY, monitors: "0" }),
  install("2026-10-20", "2026-10-26", { ...DAY, monitors: "11-50", status_pages: "true", maintenance: "true" }),
  install("2026-10-21", "2026-10-26", { ...DAY, metrics: "true" }),
  install("2026-10-22", "2026-10-26", { ...DAY, monitors: "51-200", status_pages: "true" }),
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
    activeDays: ACTIVE_DAYS,
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
    expect(html).toContain("No usage ping in this range yet.");
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
    activeDays: ACTIVE_DAYS,
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
      "Continuous installs, 7 days",
      "Continuous installs, 30 days",
      "Continuous installs, 90 days",
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
    expect(week).not.toContain('<div class="tile-note">last 7 finished days</div>');
    const day = renderDashboard({ ...data, installs: [install("2026-10-26", "2026-10-26", ACTIVE)] });
    expect(day).toContain("data covers only 1 finished day<");
  });

  test("daily active is the last finished day: 0 when it had no installs, whatever older days had", () => {
    // The overall rows say Oct 25 had 4, but the installs flagged for Oct 26 are none
    const quiet = renderDashboard({ ...data, installs: [install("2026-10-20", "2026-10-25", ACTIVE)] });
    expect(quiet).toContain('<div class="tile-value">0</div><div class="tile-note">on Oct 26</div>');
  });

  test("the daily active count does not depend on --days", () => {
    const one = renderDashboard({ ...data, days: 1 });
    expect(one).toContain('<div class="tile-value">5</div><div class="tile-note">on Oct 26</div>');
    const wide = renderDashboard({ ...data, days: 30 });
    expect(wide).toContain('<div class="tile-value">5</div><div class="tile-note">on Oct 26</div>');
  });

  test("says no finished day yet, not a coverage of 0 days, when the data starts today", () => {
    const fresh = renderDashboard({ ...data, installs: [install("2026-10-27", "2026-10-27")] });
    // Weekly, daily, monthly and the three continuous tiles
    expect(fresh.match(/<div class="tile-note">no finished day yet<\/div>/g)).toHaveLength(6);
    expect(fresh).not.toContain("covers only 0");
    expect(fresh).toContain('<div class="tile-value">–</div><div class="tile-note">daily ÷ monthly active</div>');
  });

  const tileOf = (page: string, label: string) =>
    page.match(new RegExp(`<div class="tile-label">${label}</div><div class="tile-value">([^<]*)</div><div class="tile-note">([^<]*)</div>`))!.slice(1);

  test("shows the continuous installs of the 7 day window, and a dash for windows the data does not cover", () => {
    expect(tileOf(html, "Continuous installs, 7 days")).toEqual(["4", "6 of the last 7 days or more"]);
    // The data starts on Oct 20: 7 finished days
    expect(tileOf(html, "Continuous installs, 30 days")).toEqual(["–", "needs 30 finished days, data covers 7"]);
    expect(tileOf(html, "Continuous installs, 90 days")).toEqual(["–", "needs 90 finished days, data covers 7"]);
  });

  test("shows the counts of all three windows once the data covers them", () => {
    const old = renderDashboard({ ...data, installs: [install("2026-07-01", "2026-10-26", DAY)] });
    expect(tileOf(old, "Continuous installs, 7 days")).toEqual(["4", "6 of the last 7 days or more"]);
    expect(tileOf(old, "Continuous installs, 30 days")).toEqual(["4", "26 of the last 30 days or more"]);
    expect(tileOf(old, "Continuous installs, 90 days")).toEqual(["2", "78 of the last 90 days or more"]);
  });

  test("never shows 0 for a window the data does not cover, even when the rows are empty", () => {
    const page = renderDashboard({ ...data, activeDays: [[], [], []] });
    expect(tileOf(page, "Continuous installs, 30 days")[0]).toBe("–");
    expect(tileOf(page, "Continuous installs, 7 days")[0]).toBe("0");
  });

  test("a partial 7 day window says how many finished days the data covers", () => {
    const page = renderDashboard({ ...data, installs: [install("2026-10-24", "2026-10-26", ACTIVE)] });
    expect(tileOf(page, "Continuous installs, 7 days")).toEqual(["–", "needs 7 finished days, data covers 3"]);
  });

  test("explains each continuous tile with the threshold, the window and its dates", () => {
    expect(html).toContain(
      "Distinct installs that sent a usage ping on 26 or more of the 30 days from Sep 27 to Oct 26 (UTC). These installs run every day. An install can skip some days, see Limits.",
    );
    expect(html).toContain("on 6 or more of the 7 days from Oct 20 to Oct 26 (UTC)");
    expect(html).toContain("on 78 or more of the 90 days from Jul 29 to Oct 26 (UTC)");
  });

  test("charts the weekly active installs by the days with a usage ping, from 7 days down to 1 day", () => {
    expect(html).toContain(
      '<section class="card" id="continuity"><h2>Days with a usage ping, last 7 days</h2><p class="why">Weekly active installs by the number of days with a usage ping in the last 7 finished days. Installs on 6 or 7 days run every day. Installs on 1 day ran for a short time, for example a test.</p>',
    );
    expect(html).toContain('title="7 days: 2 installs, 33%"');
    expect(html).toContain('title="6 days: 2 installs, 33%"');
    expect(html).toContain('title="2 days: 0 installs, 0%"');
    expect(html).toContain('title="1 day: 1 installs, 17%"');
    const at = ["7 days", "6 days", "5 days", "4 days", "3 days", "2 days", "1 day"].map((l) => html.indexOf(`title="${l}:`));
    expect(at.every((i) => i > 0)).toBe(true);
    expect(at).toEqual([...at].sort((a, b) => a - b));
  });

  test("never draws a bar longer than its track, even when the queries disagree by a few installs", () => {
    // One weekly active install in the summary, but 2 installs on 7 days in the histogram
    const page = renderDashboard({ ...data, installs: [install("2026-10-20", "2026-10-26", DAY)] });
    const widths = [...page.matchAll(/style="width:([\d.]+)%"/g)].map((m) => Number(m[1]));
    expect(widths.length).toBeGreaterThan(0);
    expect(Math.max(...widths)).toBe(100);
  });

  test("says so when there are no weekly active installs for the histogram", () => {
    const empty = renderDashboard({ ...data, installs: [], activeDays: [[], [], []] });
    expect(empty.slice(empty.indexOf('id="continuity"'))).toContain("No weekly active installs yet.");
  });

  test("gives every tile a one-line explanation", () => {
    expect(html.match(/<div class="tile( headline)?">/g)).toHaveLength(9);
    expect(html.match(/<div class="tile-why">/g)).toHaveLength(9);
    expect(html).toContain("Distinct installs that sent one or more usage pings from Oct 20 to Oct 26 (UTC). Each install counts one time.");
    expect(html).toContain("Distinct installs that sent a usage ping on Oct 26 (UTC).");
    expect(html).toContain("Distinct installs that sent one or more usage pings from Sep 27 to Oct 26 (UTC).");
  });

  test("shows activation and feature adoption as bars of the weekly active installs", () => {
    expect(html).toContain('title="More than 0 monitors: 5 installs, 83%"');
    expect(html).toContain('title="Status pages: 3 installs, 50%"');
    expect(html).toContain('title="Maintenance windows: 1 installs, 17%"');
    expect(html).toContain('title="Metrics endpoint: 1 installs, 17%"');
  });

  test("explains each new section in a line under its heading", () => {
    for (const [id, heading] of [
      ["growth", "New, churned and net installs per week"],
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
    expect(limits).toContain("opts in again gets a new instance ID");
    expect(limits).toContain("count it as churned and then as new");
    expect(limits).toContain("replicas share one instance ID");
    expect(limits).toContain("keeps the data for 92 days");
    expect(limits).toContain("daily count reads a few percent low");
    expect(limits).toContain("does not count an install as churned if it sends a usage ping again after 7 or more days");
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

  // The geometry of the net bar with this title, and of the zero line
  function netBar(page: string, title: string) {
    const bar = page.match(new RegExp(`<rect class="bar-net" x="[\\d.]+" y="([\\d.]+)" width="[\\d.]+" height="([\\d.]+)" rx="2"><title>${title}</title>`))!;
    const zero = page.match(/<line class="grid zero"[^>]* y1="([\d.]+)"/)!;
    return { y: Number(bar[1]), height: Number(bar[2]), zero: Number(zero[1]) };
  }

  test("charts net growth as a third series, listed in the legend, with null weeks left blank", () => {
    expect(history).toContain('<span class="sw sw-net"></span>Net growth');
    expect(history).toContain("<title>Oct 5: +4 net</title>");
    // The first week of the data has no net
    expect(history).not.toMatch(/<title>Sep 28: [^<]*net<\/title>/);
  });

  test("draws a positive net up from the zero line and a negative net below it", () => {
    const up = netBar(history, "Oct 5: \\+4 net");
    expect(up.height).toBeGreaterThan(0);
    expect(up.y + up.height).toBeCloseTo(up.zero, 0);
    const down = netBar(history, "Oct 12: -2 net");
    expect(down.height).toBeGreaterThan(0);
    expect(down.y).toBeCloseTo(down.zero, 0);
  });

  test("keeps the axis at zero when no week has a negative net", () => {
    const growing = renderDashboard({ ...data, installs: [install("2026-10-20", "2026-10-26", DAY)] });
    expect(growing).not.toMatch(/text-anchor="end">-\d/);
    // With a negative net the axis gets a negative tick
    expect(history).toMatch(/text-anchor="end">-\d/);
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
    for (const token of ["--new:", "--churned:", "--net:"]) {
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
      let rows: Record<string, unknown>[] = [{ day: "2026-10-26 00:00:00", value: "http", instances: "3" }];
      if (sql.includes("AS first_day")) rows = [install("2026-10-20", "2026-10-26", ACTIVE), install("2026-10-22", "2026-10-26", ACTIVE)];
      if (sql.startsWith("SELECT days")) rows = [{ days: "7", installs: "2" }];
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

  test("runs the ten queries, writes the page and opens it", async () => {
    const { options, bodies, written, opened } = setup();
    await run(options);
    expect(bodies).toHaveLength(10);
    expect(written).toHaveLength(1);
    expect(written[0]!.path).toBe("/tmp/out.html");
    expect(written[0]!.content).toContain("<title>Peekaping telemetry</title>");
    expect(written[0]!.content).not.toContain("tok456");
    // The two installs from the summary query reach the headline tile
    expect(written[0]!.content).toContain('<div class="tile-label">Weekly active installs</div><div class="tile-value">2</div>');
    expect(opened).toEqual(["/tmp/out.html"]);
  });

  test("sends the install summary over the 92 retained days, whatever --days is", async () => {
    const { options, bodies } = setup();
    await run({ ...options, argv: [...options.argv, "--days", "7"] });
    expect(bodies).toHaveLength(10);
    expect(bodies[0]).toContain("INTERVAL '6' DAY");
    expect(bodies[6]).toContain("AS first_day");
    expect(bodies[6]).toContain("INTERVAL '91' DAY");
  });

  test("then sends the active-days query for the 7, 30 and 90 day windows", async () => {
    const { options, bodies } = setup();
    await run({ ...options, argv: [...options.argv, "--days", "7"] });
    expect(bodies.slice(7).map((sql) => sql.match(/INTERVAL '(\d+)' DAY AND/)![1])).toEqual(["7", "30", "90"]);
    expect(bodies.slice(7).every((sql) => sql.startsWith("SELECT days, count() AS installs"))).toBe(true);
  });

  test("shows the rows of the active-days queries on the page", async () => {
    const { options, written } = setup();
    await run(options);
    expect(written[0]!.content).toContain('title="7 days: 2 installs, 100%"');
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
