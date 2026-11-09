// The SQL behind `bun run report` and `bun run dashboard`. Everything but fetchRows is free of I/O.
//
// Every report counts distinct instance IDs (index1) with count(DISTINCT ...), which Analytics Engine supports.
// Sampling is applied per index, and only to indexes with a high write volume. An instance sends about one ping
// (up to 101 points) a day, so in practice none is sampled and the count is exact.
// _sample_interval is deliberately left out: it would weight rows, not instances.
import { parseArgs } from "node:util";

export const DATASET = "peekaping_telemetry";
export const DEFAULT_DAYS = 30;
// Analytics Engine keeps data for three months
export const MAX_DAYS = 92;

export interface ReportQuery {
  title: string;
  // Header of the value column, or undefined for the overall report
  label?: string;
  sql: string;
}

function assertDays(days: unknown): asserts days is number {
  if (typeof days !== "number" || !Number.isInteger(days) || days < 1 || days > MAX_DAYS) {
    throw new Error(`days must be a whole number from 1 to ${MAX_DAYS}`);
  }
}

// The report covers `days` days: today, which is still filling up, and the days before it.
function sinceClause(days: number): string {
  return `timestamp >= toStartOfDay(NOW() - INTERVAL '${days - 1}' DAY)`;
}

export function buildQueries(days: number): ReportQuery[] {
  assertDays(days);
  const since = sinceClause(days);

  const byValue = (title: string, label: string, kind: string, column: string): ReportQuery => ({
    title: `Distinct instances per day by ${title}`,
    label,
    sql:
      `SELECT toStartOfDay(timestamp) AS day, ${column} AS value, count(DISTINCT index1) AS instances ` +
      `FROM ${DATASET} WHERE blob1 = '${kind}' AND ${since} ` +
      `GROUP BY day, value ORDER BY day, instances DESC, value`,
  });

  return [
    {
      title: "Distinct instances per day",
      sql:
        `SELECT toStartOfDay(timestamp) AS day, count(DISTINCT index1) AS instances ` +
        `FROM ${DATASET} WHERE blob1 = 'ping' AND ${since} GROUP BY day ORDER BY day`,
    },
    byValue("version", "version", "ping", "blob2"),
    byValue("mode", "mode", "ping", "blob3"),
    byValue("storage backend", "storage backend", "ping", "blob4"),
    byValue("monitor type", "monitor type", "monitor_type", "blob2"),
    byValue("notification channel type", "channel type", "notification_channel", "blob2"),
  ];
}

// One row per install seen in the retained days, for the dashboard's install metrics. It groups by index1 (the
// instance ID) without selecting it, so IDs never leave Cloudflare. It always covers MAX_DAYS, whatever --days is:
// first seen, retention and churn need the whole history. The week and month flags cover the 7 and 30 finished
// UTC days (today is left out); monitors and the feature flags come from the install's latest ping.
// Not part of buildQueries: `bun run report` doesn't use it.
export function installSummaryQuery(): string {
  const finished = (days: number) =>
    `max(if(timestamp >= toStartOfDay(NOW()) - INTERVAL '${days}' DAY AND timestamp < toStartOfDay(NOW()), 1, 0))`;
  return (
    `SELECT toStartOfDay(min(timestamp)) AS first_day, toStartOfDay(max(timestamp)) AS last_day, ` +
    `${finished(7)} AS in_week, ${finished(30)} AS in_month, ` +
    `argMax(blob6, timestamp) AS monitors, argMax(blob7, timestamp) AS status_pages, ` +
    `argMax(blob8, timestamp) AS maintenance, argMax(blob9, timestamp) AS metrics ` +
    `FROM ${DATASET} WHERE blob1 = 'ping' AND timestamp >= toStartOfDay(NOW() - INTERVAL '${MAX_DAYS - 1}' DAY) ` +
    `GROUP BY index1`
  );
}

export function parseDays(argv: string[]): number {
  const { values } = parseArgs({
    args: argv,
    options: { days: { type: "string", short: "d" } },
    strict: true,
    allowPositionals: false,
  });
  if (values.days === undefined) return DEFAULT_DAYS;
  const days = /^\d+$/.test(values.days) ? Number(values.days) : Number.NaN;
  assertDays(days);
  return days;
}

export function sqlApiRequest(accountId: string, token: string, sql: string): { url: string; init: RequestInit } {
  return {
    url: `https://api.cloudflare.com/client/v4/accounts/${accountId}/analytics_engine/sql`,
    init: { method: "POST", headers: { Authorization: `Bearer ${token}` }, body: sql },
  };
}

// Runs one query and returns its rows. The error carries the status and the API's message, never the token.
export async function fetchRows(
  fetchFn: typeof fetch,
  accountId: string,
  token: string,
  sql: string,
): Promise<Record<string, unknown>[]> {
  const { url, init } = sqlApiRequest(accountId, token, sql);
  const response = await fetchFn(url, init);
  if (!response.ok) {
    throw new Error(`The SQL API answered ${response.status}: ${(await response.text()).trim().slice(0, 500)}`);
  }
  const body = (await response.json()) as { data?: unknown };
  if (!Array.isArray(body.data)) throw new Error("The SQL API answer has no data array");
  return body.data as Record<string, unknown>[];
}

export function formatTable(query: ReportQuery, rows: Record<string, unknown>[]): string {
  if (rows.length === 0) return `${query.title}\n(no data)`;

  const columns = query.label === undefined ? ["day", "instances"] : ["day", "value", "instances"];
  const headers = query.label === undefined ? ["day", "instances"] : ["day", query.label, "instances"];
  const cells = rows.map((row) =>
    columns.map((column) => (column === "day" ? String(row[column]).slice(0, 10) : String(row[column]))),
  );
  const widths = headers.map((header, i) => Math.max(header.length, ...cells.map((row) => row[i]!.length)));
  const line = (row: string[]) =>
    row.map((cell, i) => (i === row.length - 1 ? cell.padStart(widths[i]!) : cell.padEnd(widths[i]!))).join("  ");

  return [query.title, line(headers), line(widths.map((width) => "-".repeat(width))), ...cells.map(line)].join("\n");
}
