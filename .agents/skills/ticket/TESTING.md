# Testing

## Red → green

Work one acceptance criterion at a time: write the test, watch it go **red** for the right reason, write the least code that turns it **green**, commit. For a bug, the first red test reproduces the report; it's the proof the fix fixes something.

Tests go through public seams: what a caller of the service, the HTTP API, or a user in the browser can observe. A test that mocks the code's own internals, or recomputes the expected value the way the code does, can't disagree with the code. Take expected values from the ticket, a worked example, or a literal you checked by hand.

## Where a test goes

| Change | Seam | Where |
| --- | --- | --- |
| Service or domain logic | the service's exported methods | `<module>_test.go` beside the package in `apps/server/internal/modules/<module>/` |
| HTTP behaviour: status codes, payloads, auth, validation | the API router, real services over SQLite and miniredis | `apps/server/internal/apitest/` |
| Repository or query behaviour | the repository | SQLite always; Postgres through `PEE_TEST_POSTGRES_DSN`; Mongo (`PEE_TEST_MONGO_URI`) only when the change is a Mongo fix ADR 0001 allows (see `internal/modules/persistence_test.go`) |
| Queue or worker code | the handler, with asynq on miniredis | beside the package |
| A user-visible web flow | the browser | Playwright spec in `e2e/` |

`apps/web` has no unit-test runner. Cover web logic through an e2e spec, or ask the user before adding a runner.

An e2e spec earns its cost when the ticket changes a flow a user clicks through (create, edit, delete, settings, auth). A copy or style change doesn't need one.

## Commands

Server, from `apps/server`:

```sh
go test -timeout 120s ./internal/...            # what CI runs (plus coverage)
go test -timeout 120s -run TestName ./internal/modules/<module>/
go vet ./...
```

Pass `-timeout`: a nil dereference has been seen to hang instead of panic on macOS, which stalls a run for the 10-minute default. If `go` can't find modules offline, set `GOMODCACHE=$HOME/go/pkg/mod`.

Real-database checks, when the change touches a repository. Name the containers after the ticket and let Docker pick the host ports, since other sessions may run their own:

```sh
docker run -d --rm --name pp-<n>-pg -e POSTGRES_PASSWORD=pg -p 127.0.0.1::5432 postgres:17
until docker exec pp-<n>-pg pg_isready -U postgres; do sleep 1; done
docker port pp-<n>-pg 5432      # the host port
PEE_TEST_POSTGRES_DSN='postgres://postgres:pg@127.0.0.1:<pg port>/postgres?sslmode=disable' go test -timeout 300s ./internal/...
docker stop pp-<n>-pg
```

Mongo storage is frozen under `docs/adr/0001-deprecate-mongodb-storage.md`. Start Mongo only to test a Mongo fix the ADR allows (data loss, a failed startup, a security leak). With only `PEE_TEST_MONGO_URI` set, the Postgres subtests are skipped:

```sh
docker run -d --rm --name pp-<n>-mongo -p 127.0.0.1::27017 mongo:7
until docker exec pp-<n>-mongo mongosh --quiet --eval 'db.runCommand({ping: 1})'; do sleep 1; done
docker port pp-<n>-mongo 27017      # the host port
PEE_TEST_MONGO_URI='mongodb://127.0.0.1:<mongo port>' go test -timeout 300s ./internal/...
docker stop pp-<n>-mongo
```

CI sets neither variable, so only the SQLite subtests run there; a repository change is unverified until you've run these.

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
TELEMETRY_URL=http://127.0.0.1:9/v1/ping
ERROR_REPORTING_DSN=http://public@127.0.0.1:9/1
ENV
export COMPOSE_FILE=docker-compose.dev.sqlite.yml:docker-compose.e2e.yml
docker compose up -d --build --wait --wait-timeout 120   # fails if a healthcheck doesn't pass in time
bunx playwright install chromium
PW_TEST_HTML_REPORT_OPEN=never bun run e2e
docker compose down
```

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
