import { beforeAll, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import { renderAs, text } from "@/test-support/render-with-me";
import {
  StatusHistoryBars,
  aggregateIntoBuckets,
  bucketUptimePercent,
  classifyBucket,
  computeBarGeometry,
  getStatusHistoryRangeParams,
} from "./status-history-bars";

vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

beforeAll(async () => {
  await i18n.changeLanguage("en-US");
});

describe("StatusHistoryBars helpers", () => {
  it("classifies stat buckets accurately", () => {
    expect(classifyBucket({ up: 0, down: 0, maintenance: 0 })).toBe("no_data");
    expect(classifyBucket({ up: 10, down: 0, maintenance: 0 })).toBe("up");
    expect(classifyBucket({ up: 8, down: 2, maintenance: 0 })).toBe("partial");
    expect(classifyBucket({ up: 0, down: 5, maintenance: 0 })).toBe("down");
    expect(classifyBucket({ up: 3, down: 0, maintenance: 3 })).toBe("maintenance");
  });

  it("computes bucket uptime percentages", () => {
    expect(bucketUptimePercent({ up: 0, down: 0 })).toBeNull();
    expect(bucketUptimePercent({ up: 10, down: 0 })).toBe(100);
    expect(bucketUptimePercent({ up: 3, down: 1 })).toBe(75);
    expect(bucketUptimePercent({ up: 0, down: 4 })).toBe(0);
  });

  it("computes aligned 48-bucket ranges: 48x30m (24h), 48x7h (14d), and 48x15h (30d)", () => {
    const now = Date.UTC(2026, 10, 24, 15, 30, 45);

    const r24 = getStatusHistoryRangeParams("24h", now);
    expect(r24.granularity).toBe("minute");
    expect(r24.bucketCount).toBe(48);
    expect(r24.bucketMs).toBe(30 * 60 * 1000);
    expect(r24.windowEndMs - r24.windowStartMs).toBe(24 * 60 * 60 * 1000);
    // Respects backend 1441-point limit
    expect((Date.parse(r24.until) - Date.parse(r24.since)) / 60000 + 1).toBeLessThanOrEqual(1441);

    const r14 = getStatusHistoryRangeParams("14d", now);
    expect(r14.granularity).toBe("hour");
    expect(r14.bucketCount).toBe(48);
    expect(r14.bucketMs).toBe(7 * 60 * 60 * 1000);
    expect(r14.windowEndMs - r14.windowStartMs).toBe(14 * 24 * 60 * 60 * 1000);

    const r30 = getStatusHistoryRangeParams("30d", now);
    expect(r30.granularity).toBe("hour");
    expect(r30.bucketCount).toBe(48);
    expect(r30.bucketMs).toBe(15 * 60 * 60 * 1000);
    expect(r30.windowEndMs - r30.windowStartMs).toBe(30 * 24 * 60 * 60 * 1000);
  });

  it("aggregates fine-grained points into fixed-width buckets with weighted ping", () => {
    const start = Date.UTC(2026, 10, 24, 0, 0, 0);
    const sixHours = 6 * 60 * 60 * 1000;
    const buckets = aggregateIntoBuckets(
      [
        {
          timestamp: start + 1 * 60 * 60 * 1000,
          up: 10,
          down: 0,
          maintenance: 0,
          ping: 100,
          ping_min: 80,
          ping_max: 120,
        },
        {
          timestamp: start + 2 * 60 * 60 * 1000,
          up: 30,
          down: 2,
          maintenance: 0,
          ping: 200,
          ping_min: 150,
          ping_max: 250,
        },
      ],
      { bucketCount: 4, bucketMs: sixHours, windowStartMs: start }
    );

    expect(buckets).toHaveLength(4);
    expect(buckets[0].up).toBe(40);
    expect(buckets[0].down).toBe(2);
    expect(buckets[0].ping).toBeCloseTo((10 * 100 + 30 * 200) / 40);
    expect(buckets[0].ping_min).toBe(80);
    expect(buckets[0].ping_max).toBe(250);
    expect(classifyBucket(buckets[0])).toBe("partial");
    expect(classifyBucket(buckets[1])).toBe("no_data");
  });

  it("computes uniform device-pixel-aligned widths, gaps, and offsets", () => {
    expect(computeBarGeometry(0, 48)).toBeNull();
    const desktop = computeBarGeometry(960, 56, 1);
    expect(desktop).not.toBeNull();
    expect(Number.isInteger(desktop!.segmentWidth)).toBe(true);
    expect(desktop!.gap).toBe(3);
    expect(desktop!.stripWidth).toBeLessThanOrEqual(960);

    // Fractional Windows DPI scaling (125% and 150%) lands on exact integer device pixels
    for (const dpr of [1.25, 1.5]) {
      const scaled = computeBarGeometry(900, 60, dpr);
      expect(scaled).not.toBeNull();
      expect(Number.isInteger(Math.round(scaled!.segmentWidth * dpr * 1e6) / 1e6)).toBe(true);
      expect(Number.isInteger(Math.round(scaled!.gap * dpr * 1e6) / 1e6)).toBe(true);
      expect(Number.isInteger(Math.round(scaled!.offsetLeft * dpr * 1e6) / 1e6)).toBe(true);
      expect(scaled!.stripWidth + scaled!.offsetLeft * 2).toBeLessThanOrEqual(900);
    }

    const mobile = computeBarGeometry(300, 60, 1);
    expect(mobile).not.toBeNull();
    expect(mobile!.gap).toBe(2);
    expect(mobile!.stripWidth).toBeLessThanOrEqual(300);
  });
});

describe("StatusHistoryBars component", () => {
  it("renders aligned 48-bar inset signal panels for 24h, 14d, and 30d", () => {
    const html = renderAs(
      { instanceAdmin: false, role: "owner" },
      <StatusHistoryBars id="mon-1" />
    );

    expect(html).toContain('data-testid="status-history-card"');
    expect(html).toContain('data-testid="status-history-row-24h"');
    expect(html).toContain('data-testid="status-history-row-14d"');
    expect(html).toContain('data-testid="status-history-row-30d"');
    expect(html.match(/data-testid="status-history-bar-24h"/g)).toHaveLength(48);
    expect(html.match(/data-testid="status-history-bar-14d"/g)).toHaveLength(48);
    expect(html.match(/data-testid="status-history-bar-30d"/g)).toHaveLength(48);
    expect(html).toContain(text("monitors.view.status_history.title"));
    expect(html).toContain(text("monitors.view.stats.last_24_hours"));
    expect(html).toContain(text("monitors.view.stats.last_14_days"));
    expect(html).toContain(text("monitors.view.stats.last_30_days"));
  });
});
