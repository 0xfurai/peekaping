# Development Setup

Welcome to the Peekaping development guide! Follow these steps to get your local environment up and running.

---

## 1. Clone the Repository

```bash
git clone https://github.com/0xfurai/peekaping.git
cd peekaping
```

---

## 2. Tool Management (Optional asdf support)

Peekaping supports both asdf and manual runtime installation:

### Option A: Using asdf (Recommended)

If you have [asdf](https://asdf-vm.com/) installed, you can use our automated setup:

```bash
# Run the setup target
make setup
```

This will automatically install the correct versions of Go and Node.js using asdf.

### Option B: Manual Installation

If you prefer to install tools manually:

- **Node.js**: Version **20.18.0** ([Download Node.js](https://nodejs.org/en/download/))
- **Go**: Version **1.24.1** ([Download Go](https://go.dev/dl/))
- **pnpm**: Version **9.0.0** ([Install pnpm](https://pnpm.io/installation))

Install pnpm globally:
```bash
npm install -g pnpm@9.0.0
```

Check your versions:
```bash
node -v
go version
pnpm --version
```

---

## 3. Install Dependencies

Install all project dependencies:

```bash
pnpm install
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
DB_PASSWORD=your-secure-password
DB_NAME=peekaping
DB_HOST=localhost
DB_PORT=6001
DB_TYPE=mongo # or postgres | mysql | sqlite
SERVER_PORT=8034
CLIENT_URL="http://localhost:5173"
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

You can use Docker Compose to run a local database. Example for **Postgres**:

```bash
docker compose -f docker-compose.postgres.yml up -d
```

Other options:
- `docker-compose.mongo.yml` for MongoDB

---

## 6. Start the Development Servers

Run the full stack (backend, frontend, docs) in development mode:

```bash
pnpm run dev docs:watch
```

- The web UI will be available at [http://localhost:8383](http://localhost:8383)
- The backend API will be at [http://localhost:8034](http://localhost:8034)

---

## 7. Wrapper Scripts

Peekaping includes a unified wrapper script that automatically detects if asdf is available and use it, otherwise falling back to system binaries:

- `scripts/tool.sh` - Universal wrapper for any command (go, pnpm, etc.)

This script is used throughout the project's Makefile and package.json files to ensure consistent tool usage regardless of your setup.

### Example Usage

```bash
# Using the universal wrapper
./scripts/tool.sh go test ./src/...
./scripts/tool.sh pnpm install
./scripts/tool.sh node --version
```

- API docs will be available at [http://localhost:8034/swagger/index.html](http://localhost:8034/swagger/index.html)
- Documentation will be available at [http://localhost:3000](http://localhost:3000)

---

## 8. Additional Tips

- For Go development, make sure your `GOPATH` and `PATH` are set up correctly ([Go install instructions](https://go.dev/doc/install)).

Happy hacking! 🚀
