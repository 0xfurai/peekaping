// Where a probe's availability goes on a bar: pure functions, so the edges are tested without a
// browser. A bar spans the time from `from` to `to`, and a span of time is placed on it by when it
// starts and ends, not by how many of them there are.

export interface TimeSpan {
  start: string;
  end: string;
}

export interface Placement {
  // Where the span starts and how wide it is, in percent of the bar
  leftPct: number;
  widthPct: number;
}

export type Placed<T extends TimeSpan> = T & Placement;

// The spans cut to [from, to], with their place on the bar. A span that is wholly outside the
// range, or has no length, is left out: there is nothing to draw.
export function placeSpans<T extends TimeSpan>(
  from: string,
  to: string,
  spans: T[]
): Placed<T>[] {
  const fromMs = Date.parse(from);
  const toMs = Date.parse(to);
  const total = toMs - fromMs;
  if (!(total > 0)) return [];

  const placed: Placed<T>[] = [];
  for (const span of spans) {
    const start = Math.max(Date.parse(span.start), fromMs);
    const end = Math.min(Date.parse(span.end), toMs);
    if (!(end > start)) continue;
    placed.push({
      ...span,
      leftPct: ((start - fromMs) / total) * 100,
      widthPct: ((end - start) / total) * 100,
    });
  }
  return placed;
}

// The stretch of the bar the probe was not tracked for: from the start of the range to where its
// history begins, or all of it when it has none. Null when tracking began with the range.
export function placeUntracked(
  from: string,
  to: string,
  trackedFrom: string | null
): Placement | null {
  if (trackedFrom === null) return { leftPct: 0, widthPct: 100 };

  const fromMs = Date.parse(from);
  const total = Date.parse(to) - fromMs;
  if (!(total > 0)) return null;

  const tracked = Date.parse(trackedFrom);
  if (!(tracked > fromMs)) return null;
  return {
    leftPct: 0,
    widthPct: Math.min(((tracked - fromMs) / total) * 100, 100),
  };
}

// The uptime with one decimal. It is cut, not rounded, so that a probe that was ever offline never
// reads 100.0%. The small addition keeps a value such as 99.3, which is 992.9999… times ten, from
// being cut to 99.2.
export function formatUptime(percent: number | null): string {
  if (percent === null) return "—";
  if (percent >= 100) return "100.0%";
  return `${(Math.floor(percent * 10 + 1e-9) / 10).toFixed(1)}%`;
}

// An instance with no hostname is named by the start of its id
const SHORT_ID_LENGTH = 8;

export function instanceName(instance: {
  id: string;
  hostname: string;
}): string {
  return instance.hostname || instance.id.slice(0, SHORT_ID_LENGTH);
}

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

// How long a span lasted, in its two largest units: 45s, 10m, 1h 5m, 2d 3h
export function formatDuration(ms: number): string {
  const units: [string, number][] = [
    ["d", DAY],
    ["h", HOUR],
    ["m", MINUTE],
    ["s", SECOND],
  ];
  const parts: string[] = [];
  let rest = Math.max(ms, 0);
  for (const [name, size] of units) {
    const count = Math.floor(rest / size);
    rest -= count * size;
    if (count > 0 || parts.length > 0) parts.push(`${count}${name}`);
    if (parts.length === 2) break;
  }
  const shown = parts.filter((part) => !part.startsWith("0"));
  return shown.length > 0 ? shown.join(" ") : "0s";
}
