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

export interface Metrics {
  weekly: ActiveCount;
  monthly: ActiveCount;
  // Daily active ÷ monthly active, as a fraction; undefined when either is missing or monthly is 0
  stickiness: number | undefined;
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
  };
}
