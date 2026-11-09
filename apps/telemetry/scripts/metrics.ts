// Turns the per-install summary rows (installSummaryQuery) into the dashboard's install metrics. Pure: no I/O.
// Days are UTC. "Today" is the UTC day of generatedAt and is still counting, so the last finished day is the day
// before it. Internally a day is a number of days since 1970-01-01; the output uses "YYYY-MM-DD" strings.

const DAY_MS = 86_400_000;

interface Install {
  first: number;
  last: number;
  inWeek: boolean;
  inMonth: boolean;
  monitors: string;
  statusPages: string;
  maintenance: string;
  metrics: string;
}

// A count over a window of finished days. coveredDays is set when the data starts inside the window, so the
// count can only reflect that many finished days.
export interface ActiveCount {
  count: number;
  coveredDays?: number;
}

export interface WeekRow {
  // The Monday the UTC week starts on
  week: string;
  // The current week, still counting
  soFar: boolean;
  // Installs first seen that week; null for the week the data starts in, where installs already running look new
  newInstalls: number | null;
  // An estimate: installs whose 7th silent day falls in the week
  churned: number;
  net: number | null;
}

export interface Metrics {
  weekly: ActiveCount;
  monthly: ActiveCount;
  // Daily active ÷ monthly active, as a fraction; undefined when either is missing or monthly is 0
  stickiness: number | undefined;
  weeks: WeekRow[];
}

// An install is churned once it has been silent for this many finished days.
const CHURN_DAYS = 7;

function toDay(day: number): string {
  return new Date(day * DAY_MS).toISOString().slice(0, 10);
}

// The Monday of the day's week. 1970-01-01 (day 0) was a Thursday, three days after a Monday.
function weekStart(day: number): number {
  return day - ((day + 3) % 7);
}

function toDayNumber(day: string): number {
  return Math.floor(Date.parse(`${day.slice(0, 10)}T00:00:00Z`) / DAY_MS);
}

// The SQL API returns the flags as numbers, and a day as "2026-10-21 00:00:00".
function parseInstall(row: Record<string, unknown>): Install {
  return {
    first: toDayNumber(String(row.first_day)),
    last: toDayNumber(String(row.last_day)),
    inWeek: Number(row.in_week) === 1,
    inMonth: Number(row.in_month) === 1,
    monitors: String(row.monitors),
    statusPages: String(row.status_pages),
    maintenance: String(row.maintenance),
    metrics: String(row.metrics),
  };
}

function earliestFirst(installs: Install[]): number | undefined {
  return installs.reduce<number | undefined>(
    (earliest, install) => (earliest === undefined || install.first < earliest ? install.first : earliest),
    undefined,
  );
}

// The window is the `windowDays` finished days before today.
function activeCount(count: number, windowDays: number, today: number, dataStart: number | undefined): ActiveCount {
  if (dataStart === undefined || dataStart <= today - windowDays) return { count };
  return { count, coveredDays: today - dataStart };
}

function stickiness(daily: number | undefined, monthly: number): number | undefined {
  if (daily === undefined || monthly === 0) return undefined;
  return daily / monthly;
}

function countBy(installs: Install[], key: (install: Install) => number): Map<number, number> {
  const counts = new Map<number, number>();
  for (const install of installs) counts.set(key(install), (counts.get(key(install)) ?? 0) + 1);
  return counts;
}

// New installs by the week first seen, churned installs by the week of their 7th silent day. An install that comes
// back after that is not churned: churn reads its latest ping.
function weekRows(installs: Install[], dataStart: number | undefined, today: number): WeekRow[] {
  if (dataStart === undefined) return [];
  const first = weekStart(dataStart);
  const current = weekStart(today);
  const firstSeen = countBy(installs, (install) => weekStart(install.first));
  const churned = countBy(
    installs.filter((install) => install.last + CHURN_DAYS <= today - 1),
    (install) => weekStart(install.last + CHURN_DAYS),
  );
  return Array.from({ length: (current - first) / 7 + 1 }, (_, i) => {
    const week = first + i * 7;
    // Installs already running when the data starts look new, so that week has no new or net numbers
    const added = week === first ? null : (firstSeen.get(week) ?? 0);
    const gone = churned.get(week) ?? 0;
    return { week: toDay(week), soFar: week === current, newInstalls: added, churned: gone, net: added === null ? null : added - gone };
  });
}

// dailyActive is the page's focus-day count (the latest finished day with pings); undefined when there is none.
export function computeMetrics(rows: Record<string, unknown>[], generatedAt: Date, dailyActive: number | undefined): Metrics {
  const today = toDayNumber(generatedAt.toISOString());
  const installs = rows.map(parseInstall);
  const dataStart = earliestFirst(installs);
  const weekly = installs.filter((install) => install.inWeek).length;
  const monthly = installs.filter((install) => install.inMonth).length;
  return {
    weekly: activeCount(weekly, 7, today, dataStart),
    monthly: activeCount(monthly, 30, today, dataStart),
    stickiness: stickiness(dailyActive, monthly),
    weeks: weekRows(installs, dataStart, today),
  };
}
