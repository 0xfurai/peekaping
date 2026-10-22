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

# JWT settings are now automatically managed in the database.
# Default settings are initialized on first startup:
# - Access token expiration: 15 minutes
# - Refresh token expiration: 720 hours (30 days)
# - Secret keys are automatically generated securely
```

---

## 5. Run a Database for Development

The cluster-mode binaries you run on your machine (`api`, `producer`, `worker` and `ingester`) need Redis, and with PostgreSQL or MongoDB a database server. The single-mode `peekaping` binary needs no Redis, only the database server. `docker-compose.deps.yml` runs them with Docker Compose. It always starts Redis and asynqmon, a queue dashboard, and a profile adds the database. Example for **PostgreSQL**:

```bash
docker compose -f docker-compose.deps.yml --profile postgres up -d
```

Other options:
- No profile for SQLite. It starts Redis and asynqmon and no database server, since SQLite needs none
- `--profile mongo` for MongoDB. MongoDB storage is [deprecated](./self-hosting/docker-with-mongo.md), so use it only to work on existing Mongo installs

`make docker-sqlite`, `make docker-postgres` and `make docker-mongo` run the same commands, and `make down-sqlite`, `make down-postgres` and `make down-mongo` stop them.

### Compose files

The repository root has these Docker Compose files. Each one comes in a variant per storage backend: `sqlite`, `postgres` and `mongo`, except `deps` and `e2e`.

| File | What it is for |
|------|----------------|
| `docker-compose.dev.<backend>.yml` | The whole stack in cluster mode, built from source. `make docker-dev-<backend>` starts it. The web container on `localhost:8383` calls the API on `localhost:8034`, so the `api` service sets `CORS_ORIGINS` to `http://localhost:8383` unless your `.env` sets it. |
| `docker-compose.prod.<backend>.yml` | The whole stack in cluster mode, from the published `0xfurai/peekaping-*` images, behind an nginx gateway. |
| `docker-compose.bundle.<backend>.yml` | One bundle image, built from source. The SQLite and PostgreSQL bundles run single mode; the MongoDB bundle runs cluster mode inside the image. |
| `docker-compose.deps.yml` | Redis, asynqmon and, with a profile, PostgreSQL or MongoDB, for running the Go binaries on your machine. |
| `docker-compose.e2e.yml` | The test target overlay for the Playwright suite. It adds `e2e-target` on top of a dev, prod or bundle file. |

The `mongo` files are for the deprecated MongoDB storage backend.

### Docs snippets that copy a file

The Docker pages in `apps/docs/docs/self-hosting` show the cluster-mode `docker-compose.prod.<backend>.yml` files and `nginx.conf` word for word. A comment on the line before such a code block names the file it copies, relative to the repository root:

```html
<!-- snippet: docker-compose.prod.sqlite.yml -->
```

`scripts/test-docs-snippets.sh` diffs every marked block with its file, and fails with the difference when they differ, when a marker names a file that doesn't exist, or when no code block follows a marker. The Docs Snippets workflow runs it on pull requests and pushes to `main` that change `apps/docs`, a `docker-compose*.yml` file, `nginx.conf`, the script or the workflow. So a pull request that changes one of those files updates the page in the same pull request.

Blocks with no marker aren't checked. That is for examples and excerpts that are deliberately not a full copy, such as the single-mode Compose snippets, which run the published image where `docker-compose.bundle.<backend>.yml` builds from source, and the `.env` excerpts.

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

HTTP request DTOs belong at the controller boundary. Services use domain models, and repository interfaces accept domain models or partial-update models. Each SQL or Mongo repository maps these models to its own private persistence structs; database tags belong on those structs.

Partial-update models preserve omitted fields separately from explicit zero values. A nil slice leaves an association unchanged, while an empty slice clears it. Maintenance monitor IDs are passed separately to the service so the maintenance repository only receives maintenance fields.

### Updating the API contract

REST operations use typed Huma registrations on the standard HTTP router. After changing an operation or DTO, run `bun run api:generate` and `bun run api:validate` from the repository root, then commit `apps/server/docs/openapi.json`, `openapi.yaml`, and the regenerated `apps/web/src/api` files. `bun run api:check` runs the same generation/validation and fails if committed artifacts are stale. No database or Redis process is needed for export.

`docs:watch` watches the server source and exports the spec; run `bun run --filter web gen` after contract changes to update the client. The server serves `/openapi.json`, `/openapi.yaml`, and `/docs`. `/swagger/index.html` redirects to `/docs`; `/swagger/doc.json` now serves the same OpenAPI 3.1 document. Old Swagger UI assets are removed.
