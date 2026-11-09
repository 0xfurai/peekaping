// Renders the `bun run dashboard` page: one self-contained HTML file, no external script, font or stylesheet.
// Pure: query rows in, HTML out, so it can be tested without the SQL API.
import { computeMetrics, RETENTION_DAYS, type ActiveCount, type Adoption, type CohortRow, type Metrics, type RetentionCell, type WeekRow } from "./metrics";

export interface Breakdown {
  title: string;
  label: string;
  rows: Record<string, unknown>[];
}

export interface DashboardData {
  days: number;
  // The moment the page was built. "Today" is its UTC day.
  generatedAt: Date;
  // Rows of the install summary query: one per install, never with its instance ID
  installs: Record<string, unknown>[];
  // Rows of the overall query: day, instances
  overall: Record<string, unknown>[];
  // Rows of the by-value queries: day, value, instances
  breakdowns: Breakdown[];
}

interface DayCount {
  day: string;
  instances: number;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export function escapeHtml(text: string): string {
  return text
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

// The SQL API returns "2026-10-21 00:00:00" for a day and a string for a count.
function dayOf(row: Record<string, unknown>): string {
  return String(row.day).slice(0, 10);
}

function countOf(row: Record<string, unknown>): number {
  const count = Number(row.instances);
  return Number.isFinite(count) ? count : 0;
}

function shortDay(day: string): string {
  const [, month, date] = day.split("-");
  return `${MONTHS[Number(month) - 1] ?? month} ${Number(date)}`;
}

// Every UTC day of the range, oldest first, ending today.
export function dayRange(days: number, now: Date): string[] {
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  return Array.from({ length: days }, (_, i) =>
    new Date(today - (days - 1 - i) * 86_400_000).toISOString().slice(0, 10),
  );
}

// The day the breakdowns describe: the latest finished day with pings, or today when it is the only day with any.
export function latestCompleteDay(overall: Record<string, unknown>[], today: string): string | undefined {
  const withData = overall.filter((row) => countOf(row) > 0).map(dayOf).sort();
  return withData.filter((day) => day < today).at(-1) ?? withData.at(-1);
}

// A round axis maximum: 1, 2 or 5 times a power of ten, at least the value.
export function niceMax(value: number): number {
  if (value <= 1) return 1;
  const power = 10 ** Math.floor(Math.log10(value));
  for (const step of [1, 2, 5, 10]) if (step * power >= value) return step * power;
  return 10 * power;
}

function lineChart(series: DayCount[]): string {
  const width = 720;
  const height = 240;
  const left = 44;
  const right = 16;
  const top = 16;
  const bottom = 28;
  const max = niceMax(Math.max(...series.map((point) => point.instances), 1));
  const step = series.length > 1 ? (width - left - right) / (series.length - 1) : 0;
  const x = (i: number) => left + (series.length > 1 ? i * step : (width - left - right) / 2);
  const y = (count: number) => top + (height - top - bottom) * (1 - count / max);

  const ticks = [0, max / 2, max].filter((tick, i, all) => Number.isInteger(tick) && all.indexOf(tick) === i);
  const grid = ticks
    .map(
      (tick) =>
        `<line class="grid" x1="${left}" x2="${width - right}" y1="${y(tick)}" y2="${y(tick)}"/>` +
        `<text class="tick" x="${left - 8}" y="${y(tick) + 4}" text-anchor="end">${tick}</text>`,
    )
    .join("");

  const labelAt = [...new Set([0, Math.floor((series.length - 1) / 2), series.length - 1])];
  const xLabels = labelAt
    .map((i) => {
      const anchor = i === 0 ? "start" : i === series.length - 1 ? "end" : "middle";
      return `<text class="tick" x="${x(i)}" y="${height - 8}" text-anchor="${anchor}">${shortDay(series[i]!.day)}</text>`;
    })
    .join("");

  const points = series.map((point, i) => `${x(i).toFixed(1)},${y(point.instances).toFixed(1)}`).join(" ");
  const last = series.length - 1;
  const line = series.length > 1 ? `<polyline class="line" points="${points}"/>` : "";
  const end = `<circle class="dot" cx="${x(last)}" cy="${y(series[last]!.instances)}" r="4"/>`;

  return (
    `<div class="chart" data-points='${escapeHtml(JSON.stringify(series))}'>` +
    `<svg viewBox="0 0 ${width} ${height}" role="img" aria-label="Active installs per day">` +
    grid +
    xLabels +
    line +
    end +
    `<line class="cursor" x1="0" x2="0" y1="${top}" y2="${height - bottom}" visibility="hidden"/>` +
    `<circle class="cursor-dot" r="5" visibility="hidden"/>` +
    `<rect class="hit" x="${left}" y="${top}" width="${width - left - right}" height="${height - top - bottom}"/>` +
    `</svg><div class="tooltip" hidden></div></div>`
  );
}

// A bar is as long as its count relative to `full`, the largest row unless the caller says what a full bar is.
function barChart(rows: { value: string; instances: number }[], total: number, full?: number): string {
  if (rows.length === 0) return `<p class="empty">No data for this day.</p>`;
  const max = full ?? Math.max(...rows.map((row) => row.instances), 1);
  return (
    `<div class="bars">` +
    rows
      .map((row) => {
        const share = total > 0 ? Math.round((row.instances / total) * 100) : 0;
        const width = Math.max((row.instances / max) * 100, 0.5);
        const label = escapeHtml(row.value);
        return (
          `<div class="bar-row" title="${label}: ${row.instances} installs, ${share}%">` +
          `<span class="bar-label">${label}</span>` +
          `<span class="bar-track"><span class="bar" style="width:${width.toFixed(1)}%"></span></span>` +
          `<span class="bar-value">${row.instances} <span class="muted">${share}%</span></span>` +
          `</div>`
        );
      })
      .join("") +
    `</div>`
  );
}

function table(headers: string[], rows: string[][]): string {
  if (rows.length === 0) return `<p class="empty">No data.</p>`;
  const head = headers.map((header) => `<th>${escapeHtml(header)}</th>`).join("");
  const body = rows.map((row) => `<tr>${row.map((cell) => `<td>${escapeHtml(cell)}</td>`).join("")}</tr>`).join("");
  return `<table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>`;
}

function tile(label: string, value: string, note: string, why: string, headline = false): string {
  return (
    `<div class="tile${headline ? " headline" : ""}"><div class="tile-label">${escapeHtml(label)}</div>` +
    `<div class="tile-value">${escapeHtml(value)}</div><div class="tile-note">${escapeHtml(note)}</div>` +
    `<div class="tile-why">${escapeHtml(why)}</div></div>`
  );
}

function percent(share: number): number {
  return Math.round(share * 100);
}

interface View {
  today: string;
  counts: Map<string, number>;
  series: DayCount[];
  // The finished days: today is still counting, so the chart stops at yesterday unless today is the whole range
  finished: DayCount[];
  focusDay: string | undefined;
  focusTotal: number;
}

function buildView(data: DashboardData): View {
  const range = dayRange(data.days, data.generatedAt);
  const today = range.at(-1)!;
  const counts = new Map(data.overall.map((row) => [dayOf(row), countOf(row)]));
  const series = range.map((day) => ({ day, instances: counts.get(day) ?? 0 }));
  const focusDay = latestCompleteDay(data.overall, today);
  const focusTotal = focusDay === undefined ? 0 : (counts.get(focusDay) ?? 0);
  return { today, counts, series, finished: series.length > 1 ? series.slice(0, -1) : series, focusDay, focusTotal };
}

// The note under an active-installs tile: the window, or how little of it the data covers.
function windowNote(active: ActiveCount, days: number): string {
  if (active.coveredDays === undefined) return `last ${days} finished days`;
  if (active.coveredDays === 0) return "no finished day yet";
  return `data covers only ${active.coveredDays} finished day${active.coveredDays === 1 ? "" : "s"}`;
}

// "Nov 2 – Nov 8": the `days` finished UTC days ending on lastFinished.
function windowSpan(lastFinished: string, days: number): string {
  const start = new Date(Date.parse(`${lastFinished}T00:00:00Z`) - (days - 1) * 86_400_000).toISOString().slice(0, 10);
  return `${shortDay(start)} – ${shortDay(lastFinished)}`;
}

// Always the last finished UTC day, whatever --days is and whatever the bars below show.
function dailyTile(metrics: Metrics): string {
  const note = metrics.daily.coveredDays === 0 ? windowNote(metrics.daily, 1) : `on ${shortDay(metrics.lastFinished)}`;
  const why = `Distinct installs that sent a usage ping on ${shortDay(metrics.lastFinished)} (UTC). Reads a few percent low: an install skips a day now and then (see Limits).`;
  return tile("Daily active installs", String(metrics.daily.count), note, why);
}

function stickinessTile(metrics: Metrics): string {
  const value = metrics.stickiness === undefined ? "–" : `${percent(metrics.stickiness)}%`;
  const why =
    "Of the monthly active installs, the share that was active on the last day. High means installs keep running; low means many were tried once and stopped.";
  return tile("Stickiness", value, "daily ÷ monthly active", why);
}

function activeTiles(metrics: Metrics): string[] {
  const weekly = `Distinct installs that sent at least one usage ping ${windowSpan(metrics.lastFinished, 7)} (UTC), each counted once. The best measure of how many installs are running.`;
  const monthly = `Distinct installs that sent at least one usage ping ${windowSpan(metrics.lastFinished, 30)} (UTC), including ones tried once and stopped.`;
  return [
    tile("Weekly active installs", String(metrics.weekly.count), windowNote(metrics.weekly, 7), weekly, true),
    dailyTile(metrics),
    tile("Monthly active installs", String(metrics.monthly.count), windowNote(metrics.monthly, 30), monthly),
    stickinessTile(metrics),
  ];
}

function renderTiles(data: DashboardData, view: View, metrics: Metrics): string {
  const peak = view.finished.reduce((best, point) => (point.instances > best.instances ? point : best), view.finished[0]!);
  const peakNote = peak.instances > 0 ? `on ${shortDay(peak.day)}` : "no pings yet";
  const today = "Distinct installs that sent a usage ping since 00:00 UTC today. It keeps rising until midnight UTC, so don't compare it with a full day.";
  const peakWhy = `The highest daily active installs on one finished day of the last ${data.days} days (the line chart below).`;
  return [
    ...activeTiles(metrics),
    tile("Today so far", String(view.counts.get(view.today) ?? 0), "UTC, still counting", today),
    tile(`Peak, last ${data.days} days`, String(peak.instances), peakNote, peakWhy),
  ].join("");
}

// A section card with a heading and its one-line explanation.
function section(id: string, heading: string, why: string, body: string): string {
  return `<section class="card" id="${id}"><h2>${escapeHtml(heading)}</h2><p class="why">${escapeHtml(why)}</p>${body}</section>`;
}

function signed(value: number | null): string {
  if (value === null) return "–";
  return value > 0 ? `+${value}` : String(value);
}

function weekLabel(row: { week: string; soFar: boolean }): string {
  return `${shortDay(row.week)}${row.soFar ? " (so far)" : ""}`;
}

interface WeekPlot {
  y: (count: number) => number;
  barWidth: number;
}

// The value axis of the weekly chart: from 0 up to a round maximum, and down to a round minimum when a net is negative.
function weeklyScale(weeks: WeekRow[], top: number, plotHeight: number): { y: (count: number) => number; ticks: number[] } {
  const above = niceMax(Math.max(...weeks.flatMap((w) => [w.newInstalls ?? 0, w.churned, w.net ?? 0]), 1));
  const lowest = Math.min(...weeks.map((w) => w.net ?? 0), 0);
  const below = lowest < 0 ? niceMax(-lowest) : 0;
  const ticks = [-below, 0, above / 2, above].filter((tick, i, all) => Number.isInteger(tick) && all.indexOf(tick) === i);
  return { y: (count) => top + plotHeight * (1 - (count + below) / (above + below)), ticks };
}

// A bar rises from the zero line for a positive count and hangs below it for a negative one.
function weekBar(plot: WeekPlot, cls: string, x: number, count: number, text: string): string {
  const top = plot.y(Math.max(count, 0));
  const height = Math.abs(plot.y(count) - plot.y(0));
  return `<rect class="${cls}" x="${x.toFixed(1)}" y="${top.toFixed(1)}" width="${plot.barWidth.toFixed(1)}" height="${height.toFixed(1)}" rx="2"><title>${escapeHtml(text)}</title></rect>`;
}

// New, churned (estimate) and net bars side by side around `mid`; a week without a new or net number draws no mark for it.
function weekGroup(plot: WeekPlot, week: WeekRow, mid: number, labelY: number): string {
  const label = shortDay(week.week);
  const w = plot.barWidth;
  const added = week.newInstalls === null ? "" : weekBar(plot, "bar-new", mid - 1.5 * w - 1, week.newInstalls, `${label}: ${week.newInstalls} new`);
  const gone = weekBar(plot, "bar-churned", mid - 0.5 * w, week.churned, `${label}: ${week.churned} churned (estimate)`);
  const net = week.net === null ? "" : weekBar(plot, "bar-net", mid + 0.5 * w + 1, week.net, `${label}: ${signed(week.net)} net`);
  const soFar = week.soFar ? `<text class="tick" x="${mid}" y="${labelY + 14}" text-anchor="middle">so far</text>` : "";
  return `${added}${gone}${net}<text class="tick" x="${mid}" y="${labelY}" text-anchor="middle">${label}</text>${soFar}`;
}

// Grouped bars per UTC week. Inline SVG, so no script is needed.
function weeklyChart(weeks: WeekRow[]): string {
  const width = 720;
  const height = 240;
  const left = 44;
  const right = 16;
  const top = 12;
  const bottom = 40;
  const { y, ticks } = weeklyScale(weeks, top, height - top - bottom);
  const slot = (width - left - right) / weeks.length;
  const plot = { y, barWidth: Math.min(slot * 0.26, 20) };

  const grid = ticks
    .map(
      (tick) =>
        `<line class="grid${tick === 0 ? " zero" : ""}" x1="${left}" x2="${width - right}" y1="${y(tick)}" y2="${y(tick)}"/>` +
        `<text class="tick" x="${left - 8}" y="${y(tick) + 4}" text-anchor="end">${tick}</text>`,
    )
    .join("");
  const groups = weeks.map((week, i) => weekGroup(plot, week, left + slot * (i + 0.5), height - 20)).join("");
  return `<svg viewBox="0 0 ${width} ${height}" role="img" aria-label="New, churned and net installs per week">${grid}${groups}</svg>`;
}

const LEGEND =
  `<p class="legend"><span><span class="sw sw-new"></span>New</span>` +
  `<span><span class="sw sw-churned"></span>Churned (estimate)</span>` +
  `<span><span class="sw sw-net"></span>Net growth</span></p>`;

function renderGrowth(weeks: WeekRow[]): string {
  const why =
    "Weeks run Monday to Sunday (UTC). New: installs whose first usage ping in the kept data fell in that week. Churned (estimate): installs that stopped sending the usage ping, counted in the week they reached 7 silent days; an opt-out looks the same. Net growth: new minus churned, how much the install base grew that week.";
  const rows = weeks.map((w) => [weekLabel(w), w.newInstalls === null ? "–" : String(w.newInstalls), String(w.churned), signed(w.net)]);
  const start = weeks.length > 0 ? `<p class="note">New and net are blank for ${shortDay(weeks[0]!.week)}, the week the data starts in: installs already running then look new. Weeks start on Monday (UTC).</p>` : "";
  const body = weeks.length === 0 ? `<p class="empty">No data.</p>` : LEGEND + weeklyChart(weeks);
  return section(
    "growth",
    "New, churned and net installs per week",
    why,
    `${body}<div class="scroll">${table(["week of", "new", "churned (estimate)", "net"], rows)}</div>${start}`,
  );
}

function cohortCell(cell: RetentionCell | null, size: number): string {
  if (cell === null || cell.share === null) return "";
  return `${percent(cell.share)}% (${cell.retained} of ${size})`;
}

function renderCohorts(cohorts: CohortRow[]): string {
  const why =
    "Each row is a cohort: the installs whose first usage ping fell in that week. \"After 7 days: 30% (3 of 10)\" means 3 of those 10 installs sent a usage ping 7 or more days after their first one, so they kept running at least that long. A cell stays blank until every install in the cohort is that many days old. The first week of the data is left out, since installs already running then look new.";
  const rows = cohorts.map((c) => [weekLabel({ week: c.week, soFar: false }), String(c.size), ...c.cells.map((cell) => cohortCell(cell, c.size))]);
  const headers = ["cohort, week of", "installs", ...RETENTION_DAYS.map((days) => `after ${days} day${days === 1 ? "" : "s"}`)];
  return section("cohorts", "Retention of weekly cohorts", why, `<div class="scroll">${table(headers, rows)}</div>`);
}

function renderAdoption(adoption: Adoption): string {
  const bars = (rows: { value: string; instances: number }[]) =>
    adoption.base === 0 ? `<p class="empty">No weekly active installs yet.</p>` : barChart(rows, adoption.base, adoption.base);
  const activation = section(
    "activation",
    "Activation",
    "Of the weekly active installs, how many have at least one monitor, by their latest usage ping: installs that went past setup and use Peekaping. The rest run it with nothing to check.",
    bars([{ value: "More than 0 monitors", instances: adoption.activated }]),
  );
  const features = section(
    "adoption",
    "Feature adoption",
    "Of the weekly active installs, how many use each feature, by their latest usage ping: at least one status page, at least one maintenance window, or the metrics endpoint turned on (METRICS_ENABLED).",
    bars([
      { value: "Status pages", instances: adoption.statusPages },
      { value: "Maintenance windows", instances: adoption.maintenance },
      { value: "Metrics endpoint", instances: adoption.metrics },
    ]),
  );
  return `<div class="grid2">${activation}${features}</div>`;
}

const LIMITS = [
  "Installs that opt out, or run with TELEMETRY=off, send no usage ping and are not counted.",
  "An instance ID changes when an install opts out and opts in again, so one install can count as churned and then as new.",
  "Cluster replicas share one instance ID and count once.",
  "Data is kept for 92 days, so first seen and the cohorts reach back only that far.",
  "The daily count reads a few percent low: a usage ping is due only once the last one is more than 24 hours old and the check runs hourly, so its time slides later each day and now and then skips a UTC day. Weekly and monthly counts are not affected.",
  "An install that comes back after 7 or more silent days is not counted as churned: churn reads its latest usage ping.",
];

function renderLimits(): string {
  const items = LIMITS.map((limit) => `<li>${escapeHtml(limit)}</li>`).join("");
  return section("limits", "Limits", "What these numbers can and can't tell you.", `<ul class="limits">${items}</ul>`);
}

function renderBreakdowns(data: DashboardData, view: View): string {
  return data.breakdowns
    .map((breakdown) => {
      const rows = breakdown.rows
        .filter((row) => dayOf(row) === view.focusDay)
        .map((row) => ({ value: String(row.value), instances: countOf(row) }))
        .sort((a, b) => b.instances - a.instances || a.value.localeCompare(b.value));
      return (
        `<section class="card"><h2>${escapeHtml(breakdown.label[0]!.toUpperCase() + breakdown.label.slice(1))}</h2>` +
        barChart(rows, view.focusTotal) +
        `</section>`
      );
    })
    .join("");
}

function renderTables(data: DashboardData, view: View): string {
  return (
    `<h3>Active installs per day</h3>` +
    table(
      ["day", "instances"],
      view.series.map((point) => [point.day, String(point.instances)]),
    ) +
    data.breakdowns
      .map(
        (breakdown) =>
          `<h3>${escapeHtml(breakdown.title)}</h3>` +
          table(
            ["day", breakdown.label, "instances"],
            breakdown.rows.map((row) => [dayOf(row), String(row.value), String(countOf(row))]),
          ),
      )
      .join("")
  );
}

function focusNote(view: View): string {
  if (view.focusDay === undefined) return "No pings in this range yet.";
  return `Breakdowns show ${shortDay(view.focusDay)} (UTC). Shares are of that day's ${view.focusTotal} active installs; an install can use several monitor and channel types.`;
}

const STYLES = `:root {
  color-scheme: light;
  --surface: #fcfcfb; --card: #ffffff; --border: #e6e5e1;
  --text: #0b0b0b; --text-2: #52514e; --muted: #7a7974;
  --grid: #ecebe7; --series: #2a78d6; --track: #f0efec;
  --new: #2a78d6; --churned: #d4731f; --net: #2a9d8f;
}
@media (prefers-color-scheme: dark) {
  :root {
    color-scheme: dark;
    --surface: #1a1a19; --card: #222220; --border: #363633;
    --text: #f0efec; --text-2: #c3c2b7; --muted: #94938b;
    --grid: #2f2f2c; --series: #3987e5; --track: #2c2c29;
    --new: #3987e5; --churned: #e8934a; --net: #4cc3b4;
  }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--surface); color: var(--text); font: 14px/1.45 system-ui, -apple-system, "Segoe UI", sans-serif; }
main { max-width: 1040px; margin: 0 auto; padding: 24px 16px 48px; }
header h1 { margin: 0; font-size: 22px; }
header p, .note { margin: 4px 0 0; color: var(--text-2); }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(180px, 100%), 1fr)); gap: 12px; margin: 20px 0; }
.tile, .card { background: var(--card); border: 1px solid var(--border); border-radius: 10px; padding: 14px 16px; }
.tile-label { color: var(--text-2); font-size: 13px; }
.tile-value { font-size: 32px; font-weight: 600; font-variant-numeric: tabular-nums; margin-top: 2px; }
.tile-note { color: var(--muted); font-size: 12px; }
h2 { margin: 0 0 10px; font-size: 15px; }
h3 { margin: 18px 0 6px; font-size: 13px; color: var(--text-2); }
.chart { position: relative; }
svg { display: block; width: 100%; height: auto; }
.grid { stroke: var(--grid); stroke-width: 1; }
.tick { fill: var(--muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.line { fill: none; stroke: var(--series); stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; }
.dot, .cursor-dot { fill: var(--series); stroke: var(--card); stroke-width: 2; }
.cursor { stroke: var(--muted); stroke-width: 1; }
.hit { fill: transparent; cursor: crosshair; }
.tooltip { position: absolute; pointer-events: none; transform: translate(-50%, -100%); background: var(--text); color: var(--surface); padding: 4px 8px; border-radius: 6px; font-size: 12px; white-space: nowrap; }
.grid2 { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(300px, 100%), 1fr)); gap: 12px; margin-top: 12px; }
.bars { display: grid; gap: 6px; }
.bar-row { display: grid; grid-template-columns: minmax(70px, 30%) 1fr auto; align-items: center; gap: 8px; }
.bar-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-2); }
.bar-track { background: var(--track); border-radius: 4px; height: 12px; }
.bar { display: block; height: 100%; background: var(--series); border-radius: 0 4px 4px 0; }
.bar-value { font-variant-numeric: tabular-nums; text-align: right; min-width: 64px; }
.muted, .empty { color: var(--muted); }
details { margin-top: 20px; }
summary { cursor: pointer; color: var(--text-2); }
table { border-collapse: collapse; font-variant-numeric: tabular-nums; }
th, td { text-align: left; padding: 3px 14px 3px 0; border-bottom: 1px solid var(--border); }
th { color: var(--text-2); font-weight: 500; }
main > section.card { margin-top: 12px; }
.tile.headline { border-color: var(--series); }
.tile.headline .tile-value { font-size: 40px; }
.tile-why { color: var(--muted); font-size: 12px; margin-top: 6px; }
.why { margin: 0 0 10px; color: var(--text-2); }
.legend { display: flex; flex-wrap: wrap; gap: 4px 16px; margin: 0 0 8px; color: var(--text-2); font-size: 12px; }
.sw { display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin-right: 6px; }
.sw-new { background: var(--new); }
.sw-churned { background: var(--churned); }
.sw-net { background: var(--net); }
.bar-new { fill: var(--new); }
.bar-churned { fill: var(--churned); }
.bar-net { fill: var(--net); }
.zero { stroke: var(--muted); }
.scroll { overflow-x: auto; }
ul.limits { margin: 0; padding-left: 18px; color: var(--text-2); }
ul.limits li { margin: 4px 0; }
`;

// Hover tooltips for the daily line chart
function chartScript(finished: DayCount[]): string {
  return `for (const chart of document.querySelectorAll(".chart")) {
  const points = JSON.parse(chart.dataset.points);
  const svg = chart.querySelector("svg");
  const hit = chart.querySelector(".hit");
  const cursor = chart.querySelector(".cursor");
  const dot = chart.querySelector(".cursor-dot");
  const tip = chart.querySelector(".tooltip");
  const left = Number(hit.getAttribute("x"));
  const span = Number(hit.getAttribute("width"));
  const top = Number(hit.getAttribute("y"));
  const plot = Number(hit.getAttribute("height"));
  const max = Number(${JSON.stringify(niceMax(Math.max(...finished.map((point) => point.instances), 1)))});
  const hide = () => { cursor.setAttribute("visibility", "hidden"); dot.setAttribute("visibility", "hidden"); tip.hidden = true; };
  hit.addEventListener("pointerleave", hide);
  hit.addEventListener("pointermove", (event) => {
    const box = svg.getBoundingClientRect();
    const scale = box.width / svg.viewBox.baseVal.width;
    const vx = (event.clientX - box.left) / scale;
    const i = points.length > 1 ? Math.round(((vx - left) / span) * (points.length - 1)) : 0;
    const point = points[Math.max(0, Math.min(points.length - 1, i))];
    const index = points.indexOf(point);
    const x = points.length > 1 ? left + (index * span) / (points.length - 1) : left + span / 2;
    const y = top + plot * (1 - point.instances / max);
    cursor.setAttribute("x1", x); cursor.setAttribute("x2", x); cursor.setAttribute("visibility", "visible");
    dot.setAttribute("cx", x); dot.setAttribute("cy", y); dot.setAttribute("visibility", "visible");
    tip.textContent = point.day + ": " + point.instances + " installs";
    tip.style.left = x * scale + "px";
    tip.style.top = y * scale - 10 + "px";
    tip.hidden = false;
  });
}
`;
}

export function renderDashboard(data: DashboardData): string {
  const view = buildView(data);
  const metrics = computeMetrics(data.installs, data.generatedAt);
  const generated = data.generatedAt.toISOString().slice(0, 16).replace("T", " ");

  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Peekaping telemetry</title>
<style>
${STYLES}</style>
</head>
<body>
<main>
<header>
<h1>Peekaping telemetry</h1>
<p>Distinct installs sending the usage ping, last ${data.days} days (UTC). Generated ${escapeHtml(generated)} UTC.</p>
</header>
<div class="tiles">${renderTiles(data, view, metrics)}</div>
${renderGrowth(metrics.weeks)}
${renderCohorts(metrics.cohorts)}
${renderAdoption(metrics.adoption)}
<section class="card"><h2>Active installs per day</h2>${lineChart(view.finished)}<p class="note">Distinct installs that sent a usage ping on each finished UTC day: the daily active count over time. Today is in the tile above.</p></section>
<p class="note">${escapeHtml(focusNote(view))}</p>
<div class="grid2">${renderBreakdowns(data, view)}</div>
${renderLimits()}
<details><summary>Data tables</summary>${renderTables(data, view)}</details>
</main>
<script>
${chartScript(view.finished)}</script>
</body>
</html>
`;
}
