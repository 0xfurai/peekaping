# Development Setup

Welcome to the Peekaping development guide! Follow these steps to get your local environment up and running.

---

## 1. Clone the Repository

```bash
git clone https://github.com/0xfurai/peekaping.git
cd peekaping
```

---

## 2. Install pnpm (if you don't have it)

We use [pnpm](https://pnpm.io/) for managing JavaScript dependencies. Install it globally with npm:

```bash
npm install -g pnpm
```

---

## 3. Install Dependencies

Install all project dependencies:

```bash
pnpm install
```

---

## 4. Node.js & Go Requirements

- **Node.js**: Minimum version **18** ([Download Node.js](https://nodejs.org/en/download/))
- **Go**: Minimum version **1.24** ([Download Go](https://go.dev/dl/))

Check your versions:

```bash
node -v
go version
```

---

## 5. Environment Variables

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
ACCESS_TOKEN_EXPIRED_IN=1m
ACCESS_TOKEN_SECRET_KEY=secret-key
REFRESH_TOKEN_EXPIRED_IN=60m
REFRESH_TOKEN_SECRET_KEY=secret-key
MODE=prod
TZ="America/New_York"
```

---

## 6. Run a Database for Development

You can use Docker Compose to run a local database. Example for **Postgres**:

```bash
docker compose -f docker-compose.postgres.yml up -d
```

Other options:
- `docker-compose.mongo.yml` for MongoDB

---

## 7. Start the Development Servers

Run the full stack (backend, frontend, docs) in development mode:

```bash
pnpm run dev docs:watch
```

- The web UI will be available at [http://localhost:8383](http://localhost:8383)
- The backend API will be at [http://localhost:8034](http://localhost:8034)
- Api docs will be available at [http://localhost:8034/swagger/index.html](http://localhost:8034/swagger/index.html)
- Documentation will be available at[http://localhost:3000](http://localhost:3000)

---

## 8. Additional Tips

- For Go development, make sure your `GOPATH` and `PATH` are set up correctly ([Go install instructions](https://go.dev/doc/install)).

Happy hacking! 🚀
