# Development Setup

Welcome to the Peekaping development guide! Follow these steps to get your local environment up and running.

---

## 1. Clone the Repository

```bash
git clone https://github.com/0xfurai/peekaping.git
cd peekaping
```

---

## 2. Tool Management

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

Check your versions:
```bash
node -v
go version
pnpm --version
```

### How asdf Works in This Project

Peekaping includes `.tool-versions` files that specify the exact tool versions:
- Root `.tool-versions`: Contains all tools (golang, nodejs, pnpm)
- `apps/server/.tool-versions`: Contains server-specific tools (golang, nodejs)

The project uses a universal wrapper script (`scripts/tool.sh`) that automatically:
- Uses asdf-managed tools when asdf is available
- Falls back to system tools when asdf is not installed
- Ensures consistent tool usage across all development commands

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
# Option 1: Using the Makefile (recommended)
make dev

# Option 2: Using pnpm directly
pnpm run dev docs:watch
```

- The web UI will be available at [http://localhost:8383](http://localhost:8383)
- The backend API will be at [http://localhost:8034](http://localhost:8034)

---

## 7. Wrapper Scripts & asdf Integration

Peekaping includes a unified wrapper script that automatically detects if asdf is available and uses it, otherwise falling back to system binaries:

- `scripts/tool.sh` - Universal wrapper for any command (go, pnpm, etc.)

This script is used throughout the project's Makefile and package.json files to ensure consistent tool usage regardless of your setup.

### How the Wrapper Works

The wrapper script (`scripts/tool.sh`) provides seamless integration between asdf and system tools:

1. **With asdf**: Automatically uses the versions specified in `.tool-versions`
2. **Without asdf**: Falls back to system-installed tools
3. **Error handling**: Provides clear error messages if tools are missing

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

## 8. Troubleshooting

### Common asdf Issues

**Problem**: `asdf: command not found`
```bash
# Solution: Make sure asdf is in your PATH
echo '. "$HOME/.asdf/asdf.sh"' >> ~/.zshrc  # or ~/.bashrc
source ~/.zshrc  # or ~/.bashrc
```

**Problem**: Tools not found after asdf installation
```bash
# Solution: Reshim asdf
asdf reshim golang
asdf reshim nodejs
asdf reshim pnpm
```

**Problem**: Wrong tool versions being used
```bash
# Solution: Check which version is active
asdf current

# Set the correct version
asdf local golang 1.24.1
asdf local nodejs 20.18.0
```

**Problem**: `make setup` fails
```bash
# Solution: Install asdf plugins manually
asdf plugin add golang
asdf plugin add nodejs
asdf plugin add pnpm
asdf install
```

### Manual Tool Installation Issues

- For Go development, make sure your `GOPATH` and `PATH` are set up correctly ([Go install instructions](https://go.dev/doc/install))
- Ensure Node.js and pnpm are in your system PATH
- Check that all required tools are installed with correct versions

## 9. Additional Tips

When using binaries:
- For Go development, make sure your `GOPATH` and `PATH` are set up correctly ([Go install instructions](https://go.dev/doc/install)).

When using asdf:
- Use `make setup` to automatically configure your development environment
- The wrapper script (`scripts/tool.sh`) ensures consistent tool usage across the project
- Check `.tool-versions` files to see the exact versions used in this project
- Use `asdf current` to verify your tool versions match the project requirements

Happy hacking! 🚀
