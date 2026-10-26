import { describe, expect, it } from "vitest";
import {
  formatDuration,
  formatUptime,
  instanceName,
  placeSpans,
  placeUntracked,
} from "./availability-layout";

// A day from midnight to midnight, so that an hour is 100/24 percent of the bar.
const FROM = "2026-10-24T00:00:00Z";
const TO = "2026-10-25T00:00:00Z";
const HOUR_PCT = 100 / 24;

const span = (start: string, end: string) => ({ start, end });

describe("placeSpans", () => {
  it("positions a span inside the range by its time", () => {
    const [placed] = placeSpans(FROM, TO, [
      span("2026-10-24T06:00:00Z", "2026-10-24T12:00:00Z"),
    ]);

    expect(placed.leftPct).toBeCloseTo(25, 9);
    expect(placed.widthPct).toBeCloseTo(25, 9);
  });

  it("gives a 10 minute gap in 24 hours 0.6944% of the width", () => {
    // 10 minutes is 600000 ms of 86400000: 600 / 86400 * 100 = 0.69444...%
    const [placed] = placeSpans(FROM, TO, [
      span("2026-10-24T12:00:00Z", "2026-10-24T12:10:00Z"),
    ]);

    expect(placed.leftPct).toBeCloseTo(50, 9);
    expect(placed.widthPct).toBeCloseTo(0.694444444, 6);
  });

  it("cuts a span that starts before the range at its start", () => {
    const [placed] = placeSpans(FROM, TO, [
      span("2026-10-23T23:00:00Z", "2026-10-24T02:00:00Z"),
    ]);

    expect(placed.leftPct).toBe(0);
    expect(placed.widthPct).toBeCloseTo(2 * HOUR_PCT, 9);
  });

  it("cuts a span that ends after the range at its end", () => {
    const [placed] = placeSpans(FROM, TO, [
      span("2026-10-24T22:00:00Z", "2026-10-25T03:00:00Z"),
    ]);

    expect(placed.leftPct).toBeCloseTo(22 * HOUR_PCT, 9);
    expect(placed.leftPct + placed.widthPct).toBeCloseTo(100, 9);
  });

  it("leaves out spans wholly outside the range", () => {
    const placed = placeSpans(FROM, TO, [
      span("2026-10-23T01:00:00Z", "2026-10-23T05:00:00Z"),
      span("2026-10-25T01:00:00Z", "2026-10-25T05:00:00Z"),
      // Touching the range at one instant is not in it
      span("2026-10-23T20:00:00Z", FROM),
      span(TO, "2026-10-25T05:00:00Z"),
    ]);

    expect(placed).toEqual([]);
  });

  it("leaves out a zero-length span", () => {
    const placed = placeSpans(FROM, TO, [
      span("2026-10-24T12:00:00Z", "2026-10-24T12:00:00Z"),
    ]);

    expect(placed).toEqual([]);
  });

  it("keeps what else the span carries, in order", () => {
    const placed = placeSpans(FROM, TO, [
      {
        start: "2026-10-24T00:00:00Z",
        end: "2026-10-24T06:00:00Z",
        status: "online",
      },
      {
        start: "2026-10-24T06:00:00Z",
        end: "2026-10-24T12:00:00Z",
        status: "offline",
      },
    ]);

    expect(placed.map((p) => p.status)).toEqual(["online", "offline"]);
    expect(placed[1].leftPct).toBeCloseTo(25, 9);
  });

  it("places nothing in a range that has no length", () => {
    expect(placeSpans(FROM, FROM, [span(FROM, TO)])).toEqual([]);
  });
});

describe("placeUntracked", () => {
  it("covers the stretch from the start of the range to where tracking began", () => {
    const placed = placeUntracked(FROM, TO, "2026-10-24T06:00:00Z");

    expect(placed).not.toBeNull();
    expect(placed!.leftPct).toBe(0);
    expect(placed!.widthPct).toBeCloseTo(25, 9);
  });

  it("covers the whole range when nothing is tracked", () => {
    expect(placeUntracked(FROM, TO, null)).toEqual({
      leftPct: 0,
      widthPct: 100,
    });
  });

  it("covers the whole range when tracking began after it", () => {
    expect(placeUntracked(FROM, TO, "2026-10-26T00:00:00Z")).toEqual({
      leftPct: 0,
      widthPct: 100,
    });
  });

  it("covers nothing when tracking began at or before the start of the range", () => {
    expect(placeUntracked(FROM, TO, FROM)).toBeNull();
    expect(placeUntracked(FROM, TO, "2026-10-23T00:00:00Z")).toBeNull();
  });
});

describe("formatUptime", () => {
  it("shows a dash when nothing is tracked", () => {
    expect(formatUptime(null)).toBe("—");
  });

  it("shows one decimal", () => {
    expect(formatUptime(100)).toBe("100.0%");
    expect(formatUptime(99.6)).toBe("99.6%");
    expect(formatUptime(0)).toBe("0.0%");
  });

  it("never rounds an outage up to 100%", () => {
    // 10 minutes of 24 hours is 99.3055...%
    expect(formatUptime(99.30555)).toBe("99.3%");
    expect(formatUptime(99.96)).toBe("99.9%");
  });
});

describe("formatDuration", () => {
  it("shows the two largest units", () => {
    expect(formatDuration(45_000)).toBe("45s");
    expect(formatDuration(10 * 60_000)).toBe("10m");
    expect(formatDuration(65 * 60_000)).toBe("1h 5m");
    expect(formatDuration(((2 * 24 + 3) * 60 + 20) * 60_000)).toBe("2d 3h");
  });

  it("shows under a second as 0s", () => {
    expect(formatDuration(400)).toBe("0s");
  });
});

describe("instanceName", () => {
  it("is the hostname", () => {
    expect(
      instanceName({
        id: "7c09e2d4-0000-4000-8000-000000000000",
        hostname: "fra-1",
      })
    ).toBe("fra-1");
  });

  it("is the start of the id when the instance sent no hostname", () => {
    expect(
      instanceName({
        id: "7c09e2d4-0000-4000-8000-000000000000",
        hostname: "",
      })
    ).toBe("7c09e2d4");
  });
});
