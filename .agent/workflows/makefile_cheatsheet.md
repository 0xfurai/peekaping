---
description: Makefile Cheatsheet for Vigi Project
---
# Makefile Cheatsheet

This project uses a Makefile to manage Docker environments and common tasks. ALWAYS use these make commands instead of running direct docker or go commands when possible.

## Docker Environments
- `make dev-sqlite`: Start development environment with SQLite (Recommended for local dev)
- `make dev-postgres`: Start development environment with PostgreSQL
- `make dev-mongo`: Start development environment with MongoDB
- `make switch-to-sqlite`: Switch to SQLite (stops others)

## Database Migrations
- `make migrate-up`: Run database migrations (applies to currently active DB)
- `make migrate-down`: Rollback database migrations
- `make migrate-init`: Initialize migrations

## Running the App
- `make dev`: Start all services (api, ingester, producer, worker, docs) using `pnpm` and `go run`.
- `make run-producer`: Run just the producer
- `make run-ingester`: Run just the ingester

## Testing & Linting
- `make test-server`: Run server tests
- `make lint-web`: Lint and build web app

## Setup
- `make setup`: Install tools (Go, Node, pnpm) via asdf
- `make install`: Install dependencies (pnpm install + go mod tidy)
