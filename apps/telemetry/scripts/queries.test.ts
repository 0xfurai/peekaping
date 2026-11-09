import { describe, expect, test } from "bun:test";
import { buildQueries, formatTable, installSummaryQuery, parseDays, sqlApiRequest } from "./queries";

const DATASET = "peekaping_telemetry";

// SQL is checked as written by hand, whitespace aside.
function compact(sql: string): string {
  return sql.replace(/\s+/g, " ").trim();
}

describe("buildQueries", () => {
  test("builds the six reports in order", () => {
    expect(buildQueries(30).map((q) => q.title)).toEqual([
      "Distinct instances per day",
      "Distinct instances per day by version",
      "Distinct instances per day by mode",
      "Distinct instances per day by storage backend",
      "Distinct instances per day by monitor type",
      "Distinct instances per day by notification channel type",
    ]);
  });

  test("counts distinct instances per day from the ping points", () => {
    const [overall] = buildQueries(30);
    expect(compact(overall!.sql)).toBe(
      `SELECT toStartOfDay(timestamp) AS day, count(DISTINCT index1) AS instances ` +
        `FROM ${DATASET} ` +
        `WHERE blob1 = 'ping' AND timestamp >= toStartOfDay(NOW() - INTERVAL '29' DAY) ` +
        `GROUP BY day ORDER BY day`,
    );
  });

  test.each([
    ["Distinct instances per day by version", "ping", "blob2"],
    ["Distinct instances per day by mode", "ping", "blob3"],
    ["Distinct instances per day by storage backend", "ping", "blob4"],
    ["Distinct instances per day by monitor type", "monitor_type", "blob2"],
    ["Distinct instances per day by notification channel type", "notification_channel", "blob2"],
  ])("%s groups the %s points by %s", (title, kind, column) => {
    const query = buildQueries(30).find((q) => q.title === title);
    expect(compact(query!.sql)).toBe(
      `SELECT toStartOfDay(timestamp) AS day, ${column} AS value, count(DISTINCT index1) AS instances ` +
        `FROM ${DATASET} ` +
        `WHERE blob1 = '${kind}' AND timestamp >= toStartOfDay(NOW() - INTERVAL '29' DAY) ` +
        `GROUP BY day, value ORDER BY day, instances DESC, value`,
    );
  });

  test("covers the number of days asked for, today included", () => {
    expect(compact(buildQueries(7)[0]!.sql)).toContain("INTERVAL '6' DAY");
    expect(compact(buildQueries(1)[0]!.sql)).toContain("INTERVAL '0' DAY");
    expect(compact(buildQueries(92)[0]!.sql)).toContain("INTERVAL '91' DAY");
  });

  test.each([0, -1, 1.5, Number.NaN, Number.POSITIVE_INFINITY, 93, "30" as unknown as number])(
    "refuses %p days",
    (days) => {
      expect(() => buildQueries(days)).toThrow();
    },
  );
});

describe("installSummaryQuery", () => {
  test("summarises each install over the 92 retained days", () => {
    expect(compact(installSummaryQuery())).toBe(
      `SELECT toStartOfDay(min(timestamp)) AS first_day, toStartOfDay(max(timestamp)) AS last_day, ` +
        `max(if(timestamp >= toStartOfDay(NOW()) - INTERVAL '7' DAY AND timestamp < toStartOfDay(NOW()), 1, 0)) AS in_week, ` +
        `max(if(timestamp >= toStartOfDay(NOW()) - INTERVAL '30' DAY AND timestamp < toStartOfDay(NOW()), 1, 0)) AS in_month, ` +
        `argMax(blob6, timestamp) AS monitors, argMax(blob7, timestamp) AS status_pages, ` +
        `argMax(blob8, timestamp) AS maintenance, argMax(blob9, timestamp) AS metrics ` +
        `FROM ${DATASET} WHERE blob1 = 'ping' AND timestamp >= toStartOfDay(NOW() - INTERVAL '91' DAY) ` +
        `GROUP BY index1`,
    );
  });

  test("groups by the instance ID without selecting it, so IDs stay in Cloudflare", () => {
    const select = compact(installSummaryQuery()).split(" FROM ")[0]!;
    expect(select).not.toContain("index1");
    expect(compact(installSummaryQuery())).toContain("GROUP BY index1");
  });

  test("is not part of the report", () => {
    expect(buildQueries(30).map((q) => q.sql)).not.toContain(installSummaryQuery());
  });
});

describe("parseDays", () => {
  test("defaults to 30", () => {
    expect(parseDays([])).toBe(30);
  });

  test.each([
    [["--days", "7"], 7],
    [["--days=14"], 14],
    [["-d", "92"], 92],
  ])("reads %p", (argv, days) => {
    expect(parseDays(argv)).toBe(days);
  });

  test.each([[["--days", "0"]], [["--days", "abc"]], [["--days", "1.5"]], [["--days", "93"]], [["--bogus"]], [["--days"]]])(
    "refuses %p",
    (argv) => {
      expect(() => parseDays(argv)).toThrow();
    },
  );
});

describe("sqlApiRequest", () => {
  test("posts the SQL as the body with the token as a bearer", () => {
    const request = sqlApiRequest("acct123", "tok456", "SELECT 1");
    expect(request.url).toBe("https://api.cloudflare.com/client/v4/accounts/acct123/analytics_engine/sql");
    expect(request.init.method).toBe("POST");
    expect(request.init.body).toBe("SELECT 1");
    expect(request.init.headers).toEqual({ Authorization: "Bearer tok456" });
  });
});

describe("formatTable", () => {
  const [overall, byVersion] = buildQueries(30);

  test("prints the title, an aligned table and the day without its time", () => {
    const table = formatTable(byVersion!, [
      { day: "2026-10-20 00:00:00", value: "0.0.48", instances: "12" },
      { day: "2026-10-20 00:00:00", value: "0.0.48-rc.1", instances: 3 },
      { day: "2026-10-21 00:00:00", value: "0.0.48", instances: "100" },
    ]);
    expect(table).toBe(
      [
        "Distinct instances per day by version",
        "day         version      instances",
        "----------  -----------  ---------",
        "2026-10-20  0.0.48              12",
        "2026-10-20  0.0.48-rc.1          3",
        "2026-10-21  0.0.48             100",
      ].join("\n"),
    );
  });

  test("prints the overall report without a value column", () => {
    const table = formatTable(overall!, [{ day: "2026-10-21 00:00:00", instances: "7" }]);
    expect(table).toBe(
      [
        "Distinct instances per day",
        "day         instances",
        "----------  ---------",
        "2026-10-21          7",
      ].join("\n"),
    );
  });

  test("says so when there is no data", () => {
    expect(formatTable(overall!, [])).toBe("Distinct instances per day\n(no data)");
  });
});
