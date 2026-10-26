# Testing

## Red → green

Work one acceptance criterion at a time: write the test, watch it go **red** for the right reason, write the least code that turns it **green**, commit. For a bug, the first red test reproduces the report; it's the proof the fix fixes something.

Tests go through public seams: what a caller of the service, the HTTP API, or a user in the browser can observe. A test that mocks the code's own internals, or recomputes the expected value the way the code does, can't disagree with the code. Take expected values from the ticket, a worked example, or a literal you checked by hand.

## Where a test goes

| Change | Seam | Where |
| --- | --- | --- |
| Service or domain logic | the service's exported methods | `<module>_test.go` beside the package in `apps/server/internal/modules/<module>/` |
| HTTP behaviour: status codes, payloads, auth, validation | the API router, real services over SQLite and miniredis | `apps/server/internal/apitest/` |
| Repository or query behaviour | the repository | SQLite always; Postgres through `PEE_TEST_POSTGRES_DSN` (see `internal/modules/persistence_test.go`) |
| Queue code | the handler, with asynq on miniredis | beside the package |
| A monitor or channel type's schema or codec | the registry | `registry.test.ts` beside the registry covers every listed type; add a case there only for a rule the round trip can't see |
| A user-visible web flow | the browser | Playwright spec in `e2e/` |

`apps/web` runs vitest (`bun run test`) for pure logic such as form codecs; see `docs/adr/0004-web-form-conventions.md`. A web flow a user clicks through is still covered by an e2e spec.

An e2e spec earns its cost when the ticket changes a flow a user clicks through (create, edit, delete, settings, auth). A copy or style change doesn't need one.

## Commands

Server, from `apps/server`:

```sh
go test -timeout 120s ./internal/...            # what CI runs (plus coverage)
go test -timeout 120s -run TestName ./internal/modules/<module>/
go vet ./...
go test -tags integration -count=1 -timeout 300s ./internal/modules/healthcheck/executor/...   # integration tests; need Docker
```

The `integration` tag adds tests that run against a real Redis, Kafka broker, PostgreSQL, MySQL, MongoDB (standalone and a replica set) and SQL Server, which they start in Docker, or take from `PEE_TEST_REDIS_URL`, `PEE_TEST_KAFKA_BROKER` with `PEE_TEST_KAFKA_SASL_BROKER`, `PEE_TEST_POSTGRES_EXECUTOR_URL`, `PEE_TEST_MYSQL_URL`, `PEE_TEST_MONGODB_URL` with `PEE_TEST_MONGODB_RS_URL`, and `PEE_TEST_SQLSERVER_URL` (see `apps/docs/docs/development.md`, Integration tests). CI runs them in the Integration tests workflow. Run them when the change touches a monitor executor they cover.

Pass `-timeout`: a nil dereference has been seen to hang instead of panic on macOS, which stalls a run for the 10-minute default. If `go` can't find modules offline, set `GOMODCACHE=$HOME/go/pkg/mod`.

Real-database checks, when the change touches a repository. Name the containers after the ticket and let Docker pick the host ports, since other sessions may run their own:

```sh
docker run -d --rm --name pp-<n>-pg -e POSTGRES_PASSWORD=pg -p 127.0.0.1::5432 postgres:17
until docker exec pp-<n>-pg pg_isready -U postgres; do sleep 1; done
docker port pp-<n>-pg 5432      # the host port
PEE_TEST_POSTGRES_DSN='postgres://postgres:pg@127.0.0.1:<pg port>/postgres?sslmode=disable' go test -timeout 300s ./internal/...
docker stop pp-<n>-pg
```

MongoDB storage was removed (`docs/adr/0001-deprecate-mongodb-storage.md`), so no repository runs on MongoDB. `PEE_TEST_MONGO_URI` is read only by the `bun db import-mongo` tests in `internal/mongoimport`, `cmd/bun` and `internal/apitest`. They seed a real MongoDB with raw documents of the frozen storage format (`internal/mongoimport/mongoimporttest`) and import them into SQLite and PostgreSQL. Run them when the change touches the importer. Start a PostgreSQL as above, and a MongoDB beside it, as the `import-mongo` CI job does:

```sh
docker run -d --rm --name pp-<n>-mongo -p 127.0.0.1::27017 mongo:7
until docker exec pp-<n>-mongo mongosh --quiet --eval 'db.runCommand({ping: 1})'; do sleep 1; done
docker port pp-<n>-mongo 27017      # the host port
PEE_TEST_MONGO_URI='mongodb://127.0.0.1:<mongo port>' PEE_TEST_POSTGRES_DSN='postgres://postgres:pg@127.0.0.1:<pg port>/postgres?sslmode=disable' \
  go test -timeout 300s -count=1 -v -run TestImport ./internal/mongoimport/... ./cmd/bun/ ./internal/apitest/
docker stop pp-<n>-mongo
```

A test that can't reach its database skips, so look for `SKIP` in the output. The Test workflow sets none of the variables, so only the SQLite subtests run there, and a repository change is unverified until you've run the PostgreSQL check above. The `import-mongo` job sets both variables for the import tests.

Web, from the worktree root then `apps/web`:

```sh
bun install --frozen-lockfile    # a fresh worktree has no node_modules
cd apps/web && bun run lint && bun run build
```

API contract changes (typed registrations, DTOs) need both generated layers committed with the change. From the worktree root:

```sh
bun run api:generate
bun run api:validate
bun run api:check   # verifies committed spec and client freshness
```

The server exporter uses production Huma registrations without starting databases or Redis. The pinned formatter runs as part of client generation. See `apps/server/docs/README.md` for route coverage and compatibility details.

E2E, from the worktree root. The dev stack binds fixed host ports (6379, 8034, 8383), so only one runs per machine: if `docker ps` shows another session's stack, wait for it. `docker-compose.e2e.yml` adds `e2e-target` on port 3033, the local service the tests' monitors check and notify. The stack reads a gitignored `.env`. A leftover SQLite database is fine: the setup logs in as the e2e admin (`test@test.com`) instead of registering, and fails with a message if the database has an admin with other credentials.

```sh
cat > .env <<'ENV'
DB_TYPE=sqlite
DB_NAME=/app/data/peekaping.db
REDIS_HOST=redis
REDIS_PORT=6379
SERVER_PORT=8034
CLIENT_URL=http://localhost:8383
PROBE_OFFLINE_AFTER=2m
PROBE_BOOTSTRAP_TOKEN=e2e-local-probe-token-0123456789abcdef0123456789
TELEMETRY_URL=http://127.0.0.1:9/v1/ping
ERROR_REPORTING_DSN=http://public@127.0.0.1:9/1
ENV
export COMPOSE_FILE=docker-compose.dev.sqlite.yml:docker-compose.e2e.yml
docker build -f apps/server/infra/Dockerfile.probe -t peekaping-probe apps/server   # the remote probe specs start this image
docker compose up -d --build --wait --wait-timeout 120   # fails if a healthcheck doesn't pass in time
bunx playwright install chromium
PW_TEST_HTML_REPORT_OPEN=never bun run e2e
docker compose down
```

`PROBE_OFFLINE_AFTER=2m`, the shortest the server takes, lets `e2e/remote-probe.spec.ts` wait for a stopped probe to be reported offline in about a minute and a half. That spec starts and stops `peekaping-probe` containers itself, on the stack's network (`E2E_PROBE_NETWORK` and `E2E_PROBE_URL` name them when it can't find them).

`PROBE_BOOTSTRAP_TOKEN` is the token of the local probe (at least 32 characters, any value in a test stack). The api stores it at startup, and the stack's `probe` service sends it. The `probe` service is what checks the monitors. The compose files refuse to start without it.

CI runs the suite twice against the same database, so a test must leave nothing behind that breaks its next run.

The usage ping and error reports are on by default, so a test stack must not reach Peekaping's endpoints. The two lines at the end of the `.env` above point them at an address nothing listens on (an empty `ERROR_REPORTING_DSN` would mean Peekaping's own Sentry project, so it is a dead DSN instead). The dev compose files default to the same values, and CI's `.env` sets them. A Go test that starts the app, or sets up error reporting, gives it a server or DSN of its own: `go test ./internal/reporting/` fails on one that doesn't.

UI text: every new string gets a key in `apps/web/src/i18n/locales/en-US.json` and a real translation in all 45 locale files. i18next returns the raw key for a missing entry, so `t("key") || "fallback"` shows the key to users. The files round-trip through Python `json.dumps(data, ensure_ascii=False, indent=2) + "\n"`, so a script can insert keys; check that `git diff` shows only additions.

## Full local run

CI tests each PR merged into the current `main`, so test that tree. For each branch in the stack:

```sh
git fetch origin
git switch --detach <branch> && git merge --no-edit origin/main   # throwaway merge
# server tests; web lint + build if apps/web changed; e2e if a flow changed;
# real-database checks if a repository changed
git merge --abort 2>/dev/null; git switch <branch>
```

If the merge conflicts or the merged tree fails, fix it on the branch (merge `origin/main` into it for real) before pushing.

Stacked PRs (base other than `main`) get no CI: `test.yml` and `playwright.yml` run only for PRs into `main`. Their local run is the only test they get until they're retargeted.
