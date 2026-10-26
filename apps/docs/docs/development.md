# Development Setup

Welcome to the Peekaping development guide! Follow these steps to get your local environment up and running.

---

## 1. Clone the Repository

```bash
git clone https://github.com/0xfurai/peekaping.git
cd peekaping
```

---

## 2. Install the Tools

You need Go, Node.js and Bun. Install them however you like: Homebrew, the official installers, nvm, fnm, mise or asdf all work. The repository pins each version in the file that tool reads on its own:

| Tool | What to install | Pinned in |
|------|-----------------|-----------|
| **Go** | 1.21 or newer ([Download Go](https://go.dev/dl/)) | `apps/server/go.mod`. If your Go is older than its `toolchain` line, Go switches to that version and downloads it if needed. |
| **Node.js** | 22 ([Download Node.js](https://nodejs.org/en/download/)) | `.nvmrc`. `nvm install` and `fnm install` pick it up. |
| **Bun** | The version in `packageManager` ([install options](https://bun.sh/docs/installation)) | `packageManager` in `package.json`. Bun doesn't switch to that version on its own, so `make setup` fails if yours differs. |

Bun installs the packages and runs the package scripts. Node.js still runs the tools those scripts start, such as Vite and Docusaurus. Install Bun with one of these:

```bash
curl -fsSL https://bun.sh/install | bash   # macOS and Linux
brew install oven-sh/bun/bun               # Homebrew
npm install -g bun                         # any system with Node.js
```

These install the latest release. To get the pinned version, run `npm install -g bun@<version>`. `bun upgrade` updates an existing install.

This Bun has nothing to do with `apps/server/cmd/bun`. That one is the database migration tool, built on the [uptrace/bun](https://github.com/uptrace/bun) Go library.

`make dev` also runs `docs:watch`, which exports OpenAPI from the typed Huma registrations when the server source changes. There is no separate documentation annotation generator.

Then check your setup:

```bash
make setup
```

It prints the version of each tool and how to fix anything missing or too old.

---

## 3. Install Dependencies

Install all project dependencies in apps/\{web,server\}:

```bash
make install
```

---

## 4. Environment Variables

Copy the example environment file and edit as needed:

```bash
cp .env.prod.example .env
# Edit .env with your preferred editor
```

**Common variables:**

```env
DB_USER=root
DB_PASS=your-secure-password
DB_NAME=peekaping
DB_HOST=localhost
DB_PORT=5432
DB_TYPE=postgres # or sqlite
SERVER_PORT=8034
CLIENT_URL="http://localhost:5173"
# Origins that may read the API's responses from another origin. Default: the origin of CLIENT_URL
# CORS_ORIGINS=http://localhost:8383
MODE=prod
TZ="America/New_York"

# The local probe, which checks the monitors that run on Peekaping itself.
# make dev runs a probe next to the api. The api stores PROBE_BOOTSTRAP_TOKEN as
# the local probe's token, and the probe sends PROBE_TOKEN to PEEKAPING_URL.
# The two tokens must be equal, and at least 32 characters.
PROBE_BOOTSTRAP_TOKEN=dev-local-probe-token-0123456789abcdef
PROBE_TOKEN=dev-local-probe-token-0123456789abcdef
PEEKAPING_URL=http://localhost:8034

# JWT settings are now automatically managed in the database.
# Default settings are initialized on first startup:
# - Access token expiration: 15 minutes
# - Refresh token expiration: 720 hours (30 days)
# - Secret keys are automatically generated securely
```

`.env.prod.example` leaves `PROBE_BOOTSTRAP_TOKEN` empty, so give it a value and add the other two lines. `.env.dev.example` has all three set. `make dev` starts the api, a probe and the docs watcher, so the probe checks your monitors.

---

## 5. Run a Database for Development

The cluster-mode binaries you run on your machine need Redis, and with PostgreSQL a database server. They are the `api` and the `probe` (the probe needs only the api's address, not Redis or a database). The single-mode `peekaping` binary needs no Redis, only the database server. `docker-compose.deps.yml` runs them with Docker Compose. It always starts Redis and asynqmon, a queue dashboard, and a profile adds the database. Example for **PostgreSQL**:

```bash
docker compose -f docker-compose.deps.yml --profile postgres up -d
```

Other options:
- No profile for SQLite. It starts Redis and asynqmon and no database server, since SQLite needs none

`make docker-sqlite` and `make docker-postgres` run the same commands, and `make down-sqlite` and `make down-postgres` stop them.

### Compose files

The repository root has these Docker Compose files. Each one comes in a variant per storage backend: `sqlite` and `postgres`, except `deps` and `e2e`.

| File | What it is for |
|------|----------------|
| `docker-compose.dev.<backend>.yml` | The whole stack in cluster mode, built from source. `make docker-dev-<backend>` starts it. The web container on `localhost:8383` calls the API on `localhost:8034`, so the `api` service sets `CORS_ORIGINS` to `http://localhost:8383` unless your `.env` sets it. |
| `docker-compose.prod.<backend>.yml` | The whole stack in cluster mode, from the published `0xfurai/peekaping-*` images, behind an nginx gateway. |
| `docker-compose.bundle.<backend>.yml` | One bundle image, built from source. Both bundles run single mode. |
| `docker-compose.deps.yml` | Redis, asynqmon and, with a profile, PostgreSQL, for running the Go binaries on your machine. |
| `docker-compose.e2e.yml` | The test target overlay for the Playwright suite. It adds `e2e-target` on top of a dev, prod or bundle file. |

The dev and prod files run a `probe` service, and refuse to start without `PROBE_BOOTSTRAP_TOKEN` in `.env`.

### Docs snippets that copy a file

The Docker pages in `apps/docs/docs/self-hosting` show the cluster-mode `docker-compose.prod.<backend>.yml` files and `nginx.conf` word for word. A comment on the line before such a code block names the file it copies, relative to the repository root:

```html
<!-- snippet: docker-compose.prod.sqlite.yml -->
```

`scripts/test-docs-snippets.sh` diffs every marked block with its file, and fails with the difference when they differ, when a marker names a file that doesn't exist, or when no code block follows a marker. The Docs Snippets workflow runs it on pull requests and pushes to `main` that change `apps/docs`, a `docker-compose*.yml` file, `nginx.conf`, the script or the workflow. So a pull request that changes one of those files updates the page in the same pull request.

Blocks with no marker aren't checked. That is for examples and excerpts that are deliberately not a full copy, such as the single-mode Compose snippets, which run the published image where `docker-compose.bundle.<backend>.yml` builds from source, and the `.env` excerpts.

### Integration tests

Integration tests check a monitor type against a real server instead of a stub. They carry the `integration` build tag, so `go test ./internal/...` neither compiles nor runs them, and needs no Docker. Six services have them today. `TestRedisExecutor_Integration` checks the Redis executor: Up with the right password, Down with a wrong one. `TestKafkaProducerExecutor_Integration` checks the Kafka executor against a single-node broker with a plaintext listener and a SASL listener: Up on an existing topic (the test reads the message back), with and without automatic topic creation, with SASL PLAIN, SCRAM-SHA-256 and SCRAM-SHA-512, and Down for a wrong password and for `ssl` against the plaintext listener. `TestPostgresExecutor_Integration` and `TestMySQLExecutor_Integration` check the PostgreSQL and MySQL executors against servers with TLS on and a self-signed certificate, whose user has a password with reserved characters (`@ : / % ?`): Up with the default and a custom query, Down for a wrong password, a missing database and a failing query, and each `sslmode` (PostgreSQL) or `tls` setting (MySQL) Up or Down as it should be, with a query that fails unless the connection's TLS state is the one the mode asks for.

`TestMongoDBExecutor_Integration` checks the MongoDB executor against a standalone server that requires a login: Up for the default `ping` and for a JSON path into a nested document of a real `serverStatus` reply (with and without an expected value), Down for a wrong password, a wrong `authSource`, an unknown command, a mismatch and a missing field. `TestMongoDBExecutor_ReplicaSet_Integration` uses a single-node replica set named `rs0` without authentication (a replica set with it needs a key file): Up for `?replicaSet=rs0`, Down for another name. `TestSQLServerExecutor_Integration` checks the SQL Server executor against SQL Server 2022 in both connection string forms (`Server=...;Database=...` and `sqlserver://`): Up for the default and a custom `SELECT`, Down for a wrong password, a missing database and a failing query, the certificate cases (`Encrypt=true;TrustServerCertificate=false` against the self-signed certificate is Down, `TrustServerCertificate=true` is Up) and a password with `;`, `=`, `{` and `}` (braced in the `Server=` form, percent-encoded in the URL). The cases that need no server (connection refused, a server that never answers, a query that isn't allowed, parsing a connection string) are in the executors' own `_test.go` files, such as `kafka_producer_test.go`, `postgres_test.go` and `sqlserver_test.go`, and run in the default `go test`.

The SQL Server image exists only for x86-64. On an Arm machine (Apple silicon) the tests and the compose file ask Docker for `linux/amd64`, which runs it under emulation. Measured on an Apple-silicon Mac with Docker Desktop (image already pulled), the SQL Server test took 9.9 seconds including the container start, and `docker compose up -d --wait` brought up Redis, Kafka, both MongoDB servers and SQL Server in 9 seconds; the first run also pulls the image. A Docker without x86 emulation can't run these tests.

With Docker running, run them from `apps/server`. The tests start the servers they need, each on a free port, and remove them when the run ends. `-count=1` turns off Go's test cache, which would otherwise replay an earlier pass after a server went away:

```bash
go test -tags integration -count=1 -timeout 300s ./internal/modules/healthcheck/executor/...
```

To reuse servers you started yourself, run `apps/server/docker-compose.integration.yml` and point the tests at them with the service's variable. This is what CI does. A service takes one variable (Redis, PostgreSQL, MySQL, MongoDB, SQL Server) or several (Kafka needs both of its two, the plaintext and the SASL broker). The PostgreSQL and MySQL variables hold the connection string the monitor takes, with the password percent-encoded. `PEE_TEST_POSTGRES_DSN` is another variable: the repository tests use it for a PostgreSQL of their own. A test that finds nothing at that address fails; it doesn't start a container instead. Set all of a service's variables or none: one without the others fails the test.

```bash
docker compose -f apps/server/docker-compose.integration.yml up -d --wait
cd apps/server
PEE_TEST_REDIS_URL='redis://:integration-test-password@127.0.0.1:16379' \
PEE_TEST_KAFKA_BROKER='127.0.0.1:19092' PEE_TEST_KAFKA_SASL_BROKER='127.0.0.1:19093' \
PEE_TEST_POSTGRES_EXECUTOR_URL='postgres://peekaping:p%40ss%3Aw%2Frd%251%3Fx@127.0.0.1:15432/peekaping' \
PEE_TEST_MYSQL_URL='mysql://peekaping:p%40ss%3Aw%2Frd%251%3Fx@127.0.0.1:13306/peekaping' \
PEE_TEST_MONGODB_URL='mongodb://root:integration-test-password@127.0.0.1:27018' \
PEE_TEST_MONGODB_RS_URL='mongodb://127.0.0.1:27019' \
PEE_TEST_SQLSERVER_URL='sqlserver://sa:Pe%3Be%3DK%7Bing%7D9z@127.0.0.1:11433' \
  go test -tags integration -count=1 -timeout 300s ./internal/modules/healthcheck/executor/...
docker compose -f docker-compose.integration.yml down
```

Without Docker and without the variable, a test skips and the message names both ways out. When `CI` is set, it fails instead, so a CI job can't go green by skipping.

The Integration tests workflow runs on pull requests and pushes to `main` that change `apps/server`. It runs beside Test and Lint. It also uploads the executors' statement coverage to Codecov under the `integration` flag, so a pull request shows how much of an executor these tests reach. The step that starts the services reports, in the run's summary, how long they took to become healthy; the job's own wall-clock time is on the run's page.

To add a service:

1. Add a function to `apps/server/internal/testservice`, next to `Redis`. It returns the connection details, from the service's `PEE_TEST_<SERVICE>_...` variables (a URL, or one address per listener) or from a container it starts. The shared code in `testservice.go` handles the variable, the Docker check and the skip.
2. Add the service to `apps/server/docker-compose.integration.yml`, with a health check, a fixed port on `127.0.0.1` and the same image and settings as the function.
3. Set the variables in `.github/workflows/integration.yml`.
4. Write the test beside the code it checks, with `//go:build integration` on the first line.

---

## 6. Start the Development Servers

Run the full stack (backend, frontend, docs) in development mode:

```bash
# Option 1: Using the Makefile (recommended)
make dev

# Option 2: Using Bun directly
bun run dev docs:watch
```

- The web UI will be available at [http://localhost:8383](http://localhost:8383)
- The backend API will be at [http://localhost:8034](http://localhost:8034)
- API docs will be available at [http://localhost:8034/docs](http://localhost:8034/docs)
- Documentation will be available at [http://localhost:3000](http://localhost:3000)

---

## 7. Troubleshooting

**Problem**: `make setup` says Go is on PATH but failed to run in `apps/server`

A version manager's `go` shim usually causes this when no Go version is selected for the directory, for example asdf's `No version is set for command go`. Select a Go version in your version manager, or put a regular Go install first on your `PATH`.

**Problem**: Go can't download the toolchain from `go.mod`

This happens offline, or when `GOTOOLCHAIN=local` is set. Install the version on the `toolchain` line of `apps/server/go.mod` directly from [go.dev/dl](https://go.dev/dl/).

**Problem**: `make setup` says Bun runs here, but `package.json` pins another version

Install the version from `packageManager` in `package.json`: `npm install -g bun@<version>`. `bun upgrade` only moves to the latest release.

- For Go development, make sure your `GOPATH` and `PATH` are set up correctly ([Go install instructions](https://go.dev/doc/install))
- Ensure Node.js and Bun are in your system PATH

Happy hacking! 🚀

## Server model boundaries

HTTP request DTOs belong at the controller boundary. Services use domain models, and repository interfaces accept domain models or partial-update models. Each repository maps these models to its own private persistence structs; database tags belong on those structs.

Partial-update models preserve omitted fields separately from explicit zero values. A nil slice leaves an association unchanged, while an empty slice clears it. Maintenance monitor IDs are passed separately to the service so the maintenance repository only receives maintenance fields.

### Updating the API contract

REST operations use typed Huma registrations on the standard HTTP router. After changing an operation or DTO, run `bun run api:generate` and `bun run api:validate` from the repository root, then commit `apps/server/docs/openapi.json`, `openapi.yaml`, and the regenerated `apps/web/src/api` files. `bun run api:check` runs the same generation/validation and fails if committed artifacts are stale. No database or Redis process is needed for export.

`docs:watch` watches the server source and exports the spec; run `bun run --filter web gen` after contract changes to update the client. The server serves `/openapi.json`, `/openapi.yaml`, and `/docs`. `/swagger/index.html` redirects to `/docs`; `/swagger/doc.json` now serves the same OpenAPI 3.1 document. Old Swagger UI assets are removed.
