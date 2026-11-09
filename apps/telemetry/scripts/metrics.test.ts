import { describe, expect, test } from "bun:test";
import { computeMetrics } from "./metrics";

// 2026-10-27 is a Tuesday, so today is Tuesday Oct 27, the last finished day is Monday Oct 26 and the current
// week starts on Monday Oct 26.
const NOW = new Date("2026-10-27T15:30:00Z");

// A row as the SQL API returns it: days as "YYYY-MM-DD 00:00:00" strings, the flags as JSON numbers.
function row(first: string, last: string, extra: Record<string, unknown> = {}): Record<string, unknown> {
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

describe("active installs", () => {
  test("counts the installs flagged for the week and the month, as numbers or strings", () => {
    const rows = [
      row("2026-09-01", "2026-10-26", { in_week: 1, in_month: 1 }),
      row("2026-09-01", "2026-10-22", { in_week: "1", in_month: "1" }),
      row("2026-09-01", "2026-10-10", { in_week: 0, in_month: 1 }),
      row("2026-09-01", "2026-09-20", { in_week: "0", in_month: "0" }),
    ];
    const metrics = computeMetrics(rows, NOW, undefined);
    expect(metrics.weekly.count).toBe(2);
    expect(metrics.monthly.count).toBe(3);
  });

  test("says nothing about coverage when the data starts before the window", () => {
    const metrics = computeMetrics([row("2026-09-01", "2026-10-26", { in_week: 1, in_month: 1 })], NOW, undefined);
    expect(metrics.weekly.coveredDays).toBeUndefined();
    expect(metrics.monthly.coveredDays).toBeUndefined();
  });

  test("a window that starts on the first day of the data is not partial", () => {
    // The weekly window is Oct 20 to Oct 26
    const metrics = computeMetrics([row("2026-10-20", "2026-10-26", { in_week: 1, in_month: 1 })], NOW, undefined);
    expect(metrics.weekly.coveredDays).toBeUndefined();
    expect(metrics.monthly.coveredDays).toBe(7);
  });

  test("reports the finished days the data covers when it starts inside the window", () => {
    const first = computeMetrics([row("2026-10-21", "2026-10-26", { in_week: 1, in_month: 1 })], NOW, undefined);
    expect(first.weekly.coveredDays).toBe(6);
    const latest = computeMetrics([row("2026-10-24", "2026-10-26", { in_week: 1, in_month: 1 })], NOW, undefined);
    expect(latest.weekly.coveredDays).toBe(3);
    expect(latest.monthly.coveredDays).toBe(3);
  });

  test("starts from the earliest first day, whatever the row order", () => {
    const rows = [row("2026-10-25", "2026-10-26"), row("2026-10-23", "2026-10-26")];
    expect(computeMetrics(rows, NOW, undefined).weekly.coveredDays).toBe(4);
  });

  test("is empty without rows", () => {
    const metrics = computeMetrics([], NOW, undefined);
    expect(metrics.weekly).toEqual({ count: 0 });
    expect(metrics.monthly).toEqual({ count: 0 });
    expect(metrics.stickiness).toBeUndefined();
  });
});

describe("stickiness", () => {
  const monthly = (n: number) =>
    Array.from({ length: n }, () => row("2026-09-01", "2026-10-26", { in_week: 1, in_month: 1 }));

  test("is the daily count over the monthly count", () => {
    expect(computeMetrics(monthly(8), NOW, 2).stickiness).toBe(0.25);
  });

  test("is undefined without a daily count", () => {
    expect(computeMetrics(monthly(8), NOW, undefined).stickiness).toBeUndefined();
  });

  test("is undefined when nobody was active in the month", () => {
    expect(computeMetrics([row("2026-09-01", "2026-09-05")], NOW, 3).stickiness).toBeUndefined();
  });
});

describe("weekly growth", () => {
  // Weeks (Monday starts): Sep 28, Oct 5, Oct 12, Oct 19, Oct 26. The data starts on Wed Sep 30.
  const rows = [
    row("2026-09-30", "2026-10-26"), // A: still pinging
    row("2026-09-30", "2026-10-04"), // B: last ping on a Sunday, so day 7 of silence is Sun Oct 11 (week Oct 5)
    row("2026-10-05", "2026-10-05"), // C: first seen on a Monday (week Oct 5), churns Mon Oct 12 (week Oct 12)
    row("2026-10-04", "2026-10-06"), // D: first seen on a Sunday (week Sep 28), churns Tue Oct 13
    row("2026-10-12", "2026-10-12"), // E: churns Mon Oct 19
    row("2026-10-14", "2026-10-19"), // F: last + 7 is Oct 26, the last finished day: churned
    row("2026-10-14", "2026-10-20"), // G: last + 7 is Oct 27, today: not churned yet
    row("2026-10-21", "2026-10-21"), // H: last + 7 is Oct 28
    row("2026-10-26", "2026-10-26"), // I
    row("2026-10-22", "2026-10-22"), // J
  ];
  const weeks = computeMetrics(rows, NOW, undefined).weeks;

  test("lists the UTC weeks from the first week of the data to the current one, which is marked so far", () => {
    expect(weeks.map((w) => [w.week, w.soFar])).toEqual([
      ["2026-09-28", false],
      ["2026-10-05", false],
      ["2026-10-12", false],
      ["2026-10-19", false],
      ["2026-10-26", true],
    ]);
  });

  test("leaves new and net out of the week the data starts in", () => {
    expect(weeks[0]).toMatchObject({ newInstalls: null, churned: 0, net: null });
  });

  test("counts installs by the week they were first seen in, a Sunday and a Monday apart", () => {
    expect(weeks.map((w) => w.newInstalls)).toEqual([null, 1, 3, 2, 1]);
  });

  test("counts churn in the week of the 7th silent day, only once it is a finished day", () => {
    expect(weeks.map((w) => w.churned)).toEqual([0, 1, 2, 1, 1]);
  });

  test("net growth is new minus churned", () => {
    expect(weeks.map((w) => w.net)).toEqual([null, 0, 1, 1, 0]);
  });

  test("an install that is silent for 7 days but not finished counting does not churn", () => {
    // G (last Oct 20) and H (last Oct 21) are in no week's churn: the total is 1 + 2 + 1 + 1
    expect(weeks.reduce((sum, w) => sum + w.churned, 0)).toBe(5);
  });

  test("a single week of data is the current week with no new or net numbers", () => {
    const metrics = computeMetrics([row("2026-10-27", "2026-10-27")], NOW, undefined);
    expect(metrics.weeks).toEqual([{ week: "2026-10-26", soFar: true, newInstalls: null, churned: 0, net: null }]);
  });

  test("has no weeks without rows", () => {
    expect(computeMetrics([], NOW, undefined).weeks).toEqual([]);
  });
});

describe("cohorts", () => {
  // Weeks: Sep 28 (the data starts there, via A), Oct 5, Oct 12, Oct 19, Oct 26. Last finished day: Mon Oct 26.
  const rows = [
    row("2026-09-30", "2026-10-26"), // A: the first week of the data is left out
    // Cohort Oct 5: its Sunday is Oct 11, so day 1 and day 7 are measurable (Oct 12, Oct 18), day 30 is not
    row("2026-10-05", "2026-10-05"), // pinged on day 0 only
    row("2026-10-06", "2026-10-07"), // day 1 yes (last >= Oct 7), day 7 no
    row("2026-10-11", "2026-10-18"), // day 1 yes, day 7 yes: last is exactly first + 7
    row("2026-10-08", "2026-10-14"), // day 1 yes, day 7 no
    // Cohort Oct 12: its Sunday is Oct 18
    row("2026-10-12", "2026-10-26"), // yes, yes
    row("2026-10-13", "2026-10-13"), // no, no
    row("2026-10-18", "2026-10-19"), // day 1 yes, day 7 no
    row("2026-10-14", "2026-10-14"), // no, no
    // Cohort Oct 19: its Sunday is Oct 25, so day 1 is Oct 26: the last finished day, measurable
    row("2026-10-25", "2026-10-26"),
    row("2026-10-19", "2026-10-19"),
    // Cohort Oct 26: nothing is measurable yet
    row("2026-10-26", "2026-10-26"),
  ];
  const cohorts = computeMetrics(rows, NOW, undefined).cohorts;

  test("lists every week but the first of the data, with its size", () => {
    expect(cohorts.map((c) => [c.week, c.size])).toEqual([
      ["2026-10-05", 4],
      ["2026-10-12", 4],
      ["2026-10-19", 2],
      ["2026-10-26", 1],
    ]);
  });

  test("shows the share still pinging after 1, 7 and 30 days, with the count", () => {
    expect(cohorts[0]!.cells).toEqual([{ retained: 3, share: 0.75 }, { retained: 1, share: 0.25 }, null]);
    expect(cohorts[1]!.cells).toEqual([{ retained: 2, share: 0.5 }, { retained: 1, share: 0.25 }, null]);
  });

  test("leaves a cell blank until every install in the cohort has had that many finished days", () => {
    // Day 1 of the Oct 19 cohort is Oct 26, finished; day 7 is Nov 1. Nothing in the current week is ready.
    expect(cohorts[2]!.cells).toEqual([{ retained: 1, share: 0.5 }, null, null]);
    expect(cohorts[3]!.cells).toEqual([null, null, null]);
  });

  test("measures day 30 once the cohort's Sunday + 30 is a finished day, and not a day sooner", () => {
    const old = [
      row("2026-08-03", "2026-10-26"), // first week of the data: left out
      row("2026-08-10", "2026-09-15"), // first + 30 is Sep 9: yes on all three
      row("2026-08-16", "2026-09-14"), // a Sunday; first + 30 is Sep 15: no on day 30
      row("2026-09-21", "2026-10-26"), // its Sunday is Sep 27, + 30 is Oct 27, today: day 30 not measurable
    ];
    const byWeek = new Map(computeMetrics(old, NOW, undefined).cohorts.map((c) => [c.week, c]));
    expect(byWeek.get("2026-08-10")!.cells[2]).toEqual({ retained: 1, share: 0.5 });
    expect(byWeek.get("2026-09-21")!.cells[1]).toEqual({ retained: 1, share: 1 });
    expect(byWeek.get("2026-09-21")!.cells[2]).toBeNull();
  });

  test("an empty cohort has a size of 0 and no share", () => {
    const old = [row("2026-08-03", "2026-10-26"), row("2026-08-10", "2026-08-12")];
    const empty = computeMetrics(old, NOW, undefined).cohorts.find((c) => c.week === "2026-08-17")!;
    expect(empty.size).toBe(0);
    expect(empty.cells[0]).toEqual({ retained: 0, share: null });
  });

  test("has no cohorts when the data is one week or empty", () => {
    expect(computeMetrics([row("2026-10-27", "2026-10-27")], NOW, undefined).cohorts).toEqual([]);
    expect(computeMetrics([], NOW, undefined).cohorts).toEqual([]);
  });
});
