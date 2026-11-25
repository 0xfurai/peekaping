import { describe, expect, it } from "vitest";
import { getChartRangeParams } from "./app-chart-example";

describe("getChartRangeParams", () => {
  const now = new Date("2026-11-24T12:34:56.789Z");

  it("supports short ranges with minute granularity", () => {
    const r30m = getChartRangeParams("30m", now);
    expect(r30m.granularity).toBe("minute");
    expect(Date.parse(r30m.until) - Date.parse(r30m.since)).toBe(30 * 60 * 1000);

    const r6h = getChartRangeParams("6h", now);
    expect(r6h.granularity).toBe("minute");
    expect(Date.parse(r6h.until) - Date.parse(r6h.since)).toBe(6 * 60 * 60 * 1000);
  });

  it("supports 24h, 7d, 14d, and 30d with hour granularity", () => {
    const r24h = getChartRangeParams("24h", now);
    expect(r24h.granularity).toBe("hour");
    expect(Date.parse(r24h.until) - Date.parse(r24h.since)).toBe(24 * 60 * 60 * 1000);

    const r14d = getChartRangeParams("14d", now);
    expect(r14d.granularity).toBe("hour");
    expect(Date.parse(r14d.until) - Date.parse(r14d.since)).toBe(14 * 24 * 60 * 60 * 1000);

    const r30d = getChartRangeParams("30d", now);
    expect(r30d.granularity).toBe("hour");
    expect(Date.parse(r30d.until) - Date.parse(r30d.since)).toBe(30 * 24 * 60 * 60 * 1000);
  });

  it("supports 90d with day granularity", () => {
    const r90d = getChartRangeParams("90d", now);
    expect(r90d.granularity).toBe("day");
    expect(Date.parse(r90d.until) - Date.parse(r90d.since)).toBe(90 * 24 * 60 * 60 * 1000);
  });
});
