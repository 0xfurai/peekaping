// Renders the `bun run dashboard` page: one self-contained HTML file, no external script, font or stylesheet.
// Pure: query rows in, HTML out, so it can be tested without the SQL API.

export interface Breakdown {
  title: string;
  label: string;
  rows: Record<string, unknown>[];
}

export interface DashboardData {
  days: number;
  // The moment the page was built. "Today" is its UTC day.
  generatedAt: Date;
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

function barChart(rows: { value: string; instances: number }[], total: number): string {
  if (rows.length === 0) return `<p class="empty">No data for this day.</p>`;
  const max = Math.max(...rows.map((row) => row.instances), 1);
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

function tile(label: string, value: string, note: string): string {
  return (
    `<div class="tile"><div class="tile-label">${escapeHtml(label)}</div>` +
    `<div class="tile-value">${escapeHtml(value)}</div><div class="tile-note">${escapeHtml(note)}</div></div>`
  );
}

export function renderDashboard(data: DashboardData): string {
  const range = dayRange(data.days, data.generatedAt);
  const today = range.at(-1)!;
  const counts = new Map(data.overall.map((row) => [dayOf(row), countOf(row)]));
  const series = range.map((day) => ({ day, instances: counts.get(day) ?? 0 }));
  // Today is still counting, so the chart stops at yesterday unless today is the whole range.
  const finished = series.length > 1 ? series.slice(0, -1) : series;
  const focusDay = latestCompleteDay(data.overall, today);
  const focusTotal = focusDay === undefined ? 0 : (counts.get(focusDay) ?? 0);
  const peak = finished.reduce((best, point) => (point.instances > best.instances ? point : best), finished[0]!);

  const tiles = [
    tile(
      "Active installs",
      focusDay === undefined ? "–" : String(focusTotal),
      focusDay === undefined ? "no pings yet" : `on ${shortDay(focusDay)}${focusDay === today ? ", still counting" : ""}`,
    ),
    tile("Today so far", String(counts.get(today) ?? 0), "UTC, still counting"),
    tile(`Peak, last ${data.days} days`, String(peak.instances), peak.instances > 0 ? `on ${shortDay(peak.day)}` : "no pings yet"),
  ].join("");

  const breakdowns = data.breakdowns
    .map((breakdown) => {
      const rows = breakdown.rows
        .filter((row) => dayOf(row) === focusDay)
        .map((row) => ({ value: String(row.value), instances: countOf(row) }))
        .sort((a, b) => b.instances - a.instances || a.value.localeCompare(b.value));
      return (
        `<section class="card"><h2>${escapeHtml(breakdown.label[0]!.toUpperCase() + breakdown.label.slice(1))}</h2>` +
        barChart(rows, focusTotal) +
        `</section>`
      );
    })
    .join("");

  const tables =
    `<h3>Active installs per day</h3>` +
    table(
      ["day", "instances"],
      series.map((point) => [point.day, String(point.instances)]),
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
      .join("");

  const generated = data.generatedAt.toISOString().slice(0, 16).replace("T", " ");
  const focusNote =
    focusDay === undefined
      ? "No pings in this range yet."
      : `Breakdowns show ${shortDay(focusDay)} (UTC). Shares are of that day's ${focusTotal} active installs; an install can use several monitor and channel types.`;

  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Peekaping telemetry</title>
<style>
:root {
  color-scheme: light;
  --surface: #fcfcfb; --card: #ffffff; --border: #e6e5e1;
  --text: #0b0b0b; --text-2: #52514e; --muted: #7a7974;
  --grid: #ecebe7; --series: #2a78d6; --track: #f0efec;
}
@media (prefers-color-scheme: dark) {
  :root {
    color-scheme: dark;
    --surface: #1a1a19; --card: #222220; --border: #363633;
    --text: #f0efec; --text-2: #c3c2b7; --muted: #94938b;
    --grid: #2f2f2c; --series: #3987e5; --track: #2c2c29;
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
</style>
</head>
<body>
<main>
<header>
<h1>Peekaping telemetry</h1>
<p>Distinct installs sending the usage ping, last ${data.days} days (UTC). Generated ${escapeHtml(generated)} UTC.</p>
</header>
<div class="tiles">${tiles}</div>
<section class="card"><h2>Active installs per day</h2>${lineChart(finished)}<p class="note">Finished UTC days. Today is in the tile above.</p></section>
<p class="note">${escapeHtml(focusNote)}</p>
<div class="grid2">${breakdowns}</div>
<details><summary>Data tables</summary>${tables}</details>
</main>
<script>
for (const chart of document.querySelectorAll(".chart")) {
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
</script>
</body>
</html>
`;
}
