// `bun run report [--days N]`: distinct instances per day, from the Analytics Engine SQL API.
// Needs CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_ANALYTICS_TOKEN (a token with Account > Account Analytics > Read).
import { buildQueries, formatTable, parseDays, sqlApiRequest } from "./queries";

export interface RunOptions {
  argv: string[];
  env: Record<string, string | undefined>;
  fetch: typeof fetch;
  print: (text: string) => void;
}

function requireEnv(env: RunOptions["env"], name: string): string {
  const value = env[name];
  if (!value) throw new Error(`${name} is not set`);
  return value;
}

async function query(options: RunOptions, accountId: string, token: string, sql: string) {
  const { url, init } = sqlApiRequest(accountId, token, sql);
  const response = await options.fetch(url, init);
  if (!response.ok) {
    throw new Error(`The SQL API answered ${response.status}: ${(await response.text()).trim().slice(0, 500)}`);
  }
  const body = (await response.json()) as { data?: unknown };
  if (!Array.isArray(body.data)) throw new Error("The SQL API answer has no data array");
  return body.data as Record<string, unknown>[];
}

export async function run(options: RunOptions): Promise<void> {
  const days = parseDays(options.argv);
  const accountId = requireEnv(options.env, "CLOUDFLARE_ACCOUNT_ID");
  const token = requireEnv(options.env, "CLOUDFLARE_ANALYTICS_TOKEN");

  options.print(`Distinct instances per day, last ${days} days (UTC, today still counting)\n`);
  for (const report of buildQueries(days)) {
    options.print(formatTable(report, await query(options, accountId, token, report.sql)));
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
