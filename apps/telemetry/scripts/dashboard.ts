// `bun run dashboard [--days N] [--out FILE] [--no-open]`: writes the usage ping report as one HTML page with charts
// and opens it. Same queries and credentials as `bun run report`; the page is a snapshot, rebuilt on each run.
// Needs CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_ANALYTICS_TOKEN (a token with Account > Account Analytics > Read).
import { resolve } from "node:path";
import { parseArgs } from "node:util";
import { renderDashboard } from "./dashboard-html";
import { buildQueries, fetchRows, parseDays } from "./queries";
import { requireEnv } from "./report";

export const DEFAULT_OUT = "dashboard.html";

export interface DashboardOptions {
  argv: string[];
  env: Record<string, string | undefined>;
  fetch: typeof fetch;
  now: () => Date;
  writeFile: (path: string, content: string) => Promise<void>;
  // Opens the written file in a browser
  open: (path: string) => void;
  print: (text: string) => void;
}

export function parseDashboardArgs(argv: string[]): { days: number; out: string; open: boolean } {
  const { values } = parseArgs({
    args: argv,
    options: {
      days: { type: "string", short: "d" },
      out: { type: "string", short: "o" },
      "no-open": { type: "boolean" },
    },
    strict: true,
    allowPositionals: false,
  });
  const days = parseDays(values.days === undefined ? [] : ["--days", values.days]);
  return { days, out: values.out ?? DEFAULT_OUT, open: !values["no-open"] };
}

export async function run(options: DashboardOptions): Promise<string> {
  const args = parseDashboardArgs(options.argv);
  const accountId = requireEnv(options.env, "CLOUDFLARE_ACCOUNT_ID");
  const token = requireEnv(options.env, "CLOUDFLARE_ANALYTICS_TOKEN");

  const [overall, ...byValue] = buildQueries(args.days);
  const results = await Promise.all(
    [overall!, ...byValue].map((query) => fetchRows(options.fetch, accountId, token, query.sql)),
  );
  const html = renderDashboard({
    days: args.days,
    generatedAt: options.now(),
    overall: results[0]!,
    breakdowns: byValue.map((query, i) => ({ title: query.title, label: query.label!, rows: results[i + 1]! })),
  });

  const path = resolve(args.out);
  await options.writeFile(path, html);
  options.print(`Wrote ${path}`);
  if (args.open) options.open(path);
  return path;
}

function openInBrowser(path: string): void {
  const command = process.platform === "darwin" ? "open" : process.platform === "win32" ? "explorer" : "xdg-open";
  try {
    Bun.spawn([command, path], { stdout: "ignore", stderr: "ignore" });
  } catch {
    console.log("Open it in a browser.");
  }
}

if (import.meta.main) {
  try {
    await run({
      argv: process.argv.slice(2),
      env: process.env,
      fetch,
      now: () => new Date(),
      writeFile: async (path, content) => void (await Bun.write(path, content)),
      open: openInBrowser,
      print: console.log,
    });
  } catch (error) {
    console.error(error instanceof Error ? error.message : error);
    process.exit(1);
  }
}
