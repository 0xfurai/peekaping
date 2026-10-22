# Peekaping telemetry Worker

A Cloudflare Worker at `https://telemetry.peekaping.com/v1/ping`. It receives the anonymous daily **usage ping** that self-hosted Peekaping servers send (on by default, and an admin can opt out), and writes it to Workers Analytics Engine (dataset `peekaping_telemetry`, kept for 3 months). The rules behind it are in [ADR 0003](../../docs/adr/0003-telemetry-and-error-reporting.md).

## The endpoint

`POST /v1/ping` with a JSON body of at most 4096 bytes. Every field is required and nothing else is allowed:

```json
{
  "instance_id": "0b8f6c1e-5d3a-4b7e-9c2a-1f4e8d7a6b50",
  "version": "0.0.48",
  "mode": "single",
  "db": "sqlite",
  "os": "linux/arm64",
  "monitors": "11-50",
  "monitor_types": ["http", "ping", "docker"],
  "notification_providers": ["telegram", "slack"],
  "features": { "status_pages": true, "maintenance": false, "metrics": true }
}
```

A valid ping gets `204`. Another path gets `404`, another method `405`, a body that is too large, not JSON or not valid `400`. Nothing is stored for those, and the response never repeats the input. The validator is `src/validate.ts`.

For each valid ping the Worker writes one data point with the instance ID as the index and the fields as blobs (`ping`, version, mode, db, os, monitors bucket, three feature flags), plus one point per monitor type (`monitor_type`, type) and one per notification channel type (`notification_channel`, type). That is at most 101 points, under the limit of 250 per invocation. The client IP, country and `request.cf` are never read, and nothing of the request is stored but those validated fields. Of the request the Worker reads only the method, the path, the `Content-Length` header and the body, to route it and turn away a body that is too large. Logs, traces and Logpush are off in `wrangler.jsonc`.

## Develop

```sh
bun install --frozen-lockfile --filter telemetry   # from the repo root
cd apps/telemetry
bun test                # validator, handler (fake dataset binding) and report tests
bun run check-types
bun run dev             # wrangler dev; writes to the local Analytics Engine stub
```

## Deploy

Pushes to `main` that touch `apps/telemetry/**` deploy the Worker through `.github/workflows/telemetry.yml`, after the tests pass. The deploy job runs in the `cloudflare` environment with the `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` secrets, as `deploy-sites.yml` does. Pull requests only run the tests and never see the secrets.

To deploy by hand: `bun run deploy` with those two variables set. The dataset is created by the first write.

## Report

```sh
export CLOUDFLARE_ACCOUNT_ID=...
export CLOUDFLARE_ANALYTICS_TOKEN=...   # a token with Account > Account Analytics > Read
bun run report                          # the last 30 days; --days N for 1 to 92
```

It queries the [Analytics Engine SQL API](https://developers.cloudflare.com/analytics/analytics-engine/sql-api/) and prints distinct instances per day, overall and by version, mode, storage backend, monitor type and notification channel type. Days are UTC, and today is still counting.

The counts are exact: the SQL API supports `count(DISTINCT index1)`, and the index is the instance ID. Analytics Engine samples per index, and only an index with a high write volume is sampled. An instance sends about one ping a day (up to 101 points), so in practice none is sampled and the report ignores `_sample_interval`. Before the first ping arrives, the dataset doesn't exist and the SQL API fails the query.

The deploy token is not the report token. Create the report token with only the read permission.
