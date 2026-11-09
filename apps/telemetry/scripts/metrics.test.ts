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
