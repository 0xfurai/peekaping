// `bun run report [--days N]`: distinct instances per day, from the Analytics Engine SQL API.
// Needs CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_ANALYTICS_TOKEN (a token with Account > Account Analytics > Read).
import { buildQueries, fetchRows, formatTable, parseDays } from "./queries";

export interface RunOptions {
  argv: string[];
  env: Record<string, string | undefined>;
  fetch: typeof fetch;
  print: (text: string) => void;
}

export function requireEnv(env: RunOptions["env"], name: string): string {
  const value = env[name];
  if (!value) throw new Error(`${name} is not set`);
  return value;
}

export async function run(options: RunOptions): Promise<void> {
  const days = parseDays(options.argv);
  const accountId = requireEnv(options.env, "CLOUDFLARE_ACCOUNT_ID");
  const token = requireEnv(options.env, "CLOUDFLARE_ANALYTICS_TOKEN");

  options.print(`Distinct instances per day, last ${days} days (UTC, today still counting)\n`);
  for (const report of buildQueries(days)) {
    options.print(formatTable(report, await fetchRows(options.fetch, accountId, token, report.sql)));
    options.print("");
  }
}

if (import.meta.main) {
  try {
    await run({ argv: process.argv.slice(2), env: process.env, fetch, print: console.log });
  } catch (error) {
    console.error(error instanceof Error ? error.message : error);
    process.exit(1);
  }
}
