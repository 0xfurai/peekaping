# Makefile for Peekaping project
#
# This Makefile supports multiple Docker Compose configurations:
# - Development: docker-dev-postgres, docker-dev-sqlite
# - Cluster mode: docker-cluster-postgres, docker-cluster-sqlite
# - Dependencies: docker-sqlite, docker-postgres
#
# To change the default database, modify DEFAULT_DEV_DB or DEFAULT_CLUSTER_DB below
# Example: make docker-down (stops the default development services) vs make down-dev-sqlite (a specific database)

# Variables
GO_SERVER_DIR = apps/server
WEB_DIR = apps/web
BINARY_NAME = peekaping-server

# Docker Compose configurations
COMPOSE_DEV_POSTGRES = docker-compose.dev.postgres.yml
COMPOSE_DEV_SQLITE = docker-compose.dev.sqlite.yml
COMPOSE_CLUSTER_POSTGRES = docker-compose.cluster.postgres.yml
COMPOSE_CLUSTER_SQLITE = docker-compose.cluster.sqlite.yml
COMPOSE_DEPS = docker-compose.deps.yml

# Default configurations
DEFAULT_DEV_DB = postgres
DEFAULT_CLUSTER_DB = postgres

# Default target
.DEFAULT_GOAL := help

# Help target - shows available commands
.PHONY: help
help: ## Show this help message
	@echo "📋 Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2}'

.PHONY: docker-configs
docker-configs: ## Show all available Docker Compose configurations
	@echo "📋 Available Docker Compose Configurations:"
	@echo ""
	@echo "🔧 \033[32mDEVELOPMENT ENVIRONMENTS:\033[0m"
	@echo "  make docker-dev-postgres   # $(COMPOSE_DEV_POSTGRES)"
	@echo "  make docker-dev-sqlite     # $(COMPOSE_DEV_SQLITE)"
	@echo ""
	@echo "🚀 \033[33mCLUSTER MODE:\033[0m"
	@echo "  make docker-cluster-postgres # $(COMPOSE_CLUSTER_POSTGRES)"
	@echo "  make docker-cluster-sqlite   # $(COMPOSE_CLUSTER_SQLITE)"
	@echo ""
	@echo "🎯 \033[36mDEPENDENCIES:\033[0m"
	@echo "  make docker-sqlite         # $(COMPOSE_DEPS)"
	@echo "  make docker-postgres       # $(COMPOSE_DEPS) --profile postgres"
	@echo ""
	@echo "⚡ \033[35mQUICK SWITCHERS:\033[0m"
	@echo "  make switch-to-postgres    # Stop all → Start PostgreSQL dev"
	@echo "  make switch-to-sqlite      # Stop all → Start SQLite dev"
	@echo ""
	@echo "🛑 \033[31mSTOP:\033[0m"
	@echo "  make down-dev-postgres     # $(COMPOSE_DEV_POSTGRES)"
	@echo "  make down-dev-sqlite       # $(COMPOSE_DEV_SQLITE)"
	@echo "  make down-cluster-postgres # $(COMPOSE_CLUSTER_POSTGRES)"
	@echo "  make down-cluster-sqlite   # $(COMPOSE_CLUSTER_SQLITE)"
	@echo "  make down-postgres         # $(COMPOSE_DEPS) --profile postgres"
	@echo "  make down-sqlite           # $(COMPOSE_DEPS)"
	@echo "  make docker-down-all       # Stop all configurations"


# Docker targets - Development Environment
.PHONY: docker-dev-postgres
docker-dev-postgres: ## Start development environment with PostgreSQL
	@echo "Starting development environment with PostgreSQL..."
	docker compose -f $(COMPOSE_DEV_POSTGRES) up -d --build

.PHONY: docker-dev-sqlite
docker-dev-sqlite: ## Start development environment with SQLite
	@echo "Starting development environment with SQLite..."
	docker compose -f $(COMPOSE_DEV_SQLITE) up -d --build


# Docker targets - Cluster mode
.PHONY: docker-cluster-postgres
docker-cluster-postgres: ## Start cluster mode with PostgreSQL
	@echo "Starting cluster mode with PostgreSQL..."
	docker compose -f $(COMPOSE_CLUSTER_POSTGRES) up -d

.PHONY: docker-cluster-sqlite
docker-cluster-sqlite: ## Start cluster mode with SQLite
	@echo "Starting cluster mode with SQLite..."
	docker compose -f $(COMPOSE_CLUSTER_SQLITE) up -d


# Docker targets - Standard Configurations
.PHONY: docker-postgres
docker-postgres: ## Start PostgreSQL environment
	@echo "Starting PostgreSQL environment..."
	docker compose -f $(COMPOSE_DEPS) --profile postgres up -d

.PHONY: docker-sqlite
docker-sqlite: ## Start SQLite environment
	@echo "Starting SQLite environment..."
	docker compose -f $(COMPOSE_DEPS) up -d

# Docker targets - Service Management
.PHONY: down-dev-postgres
down-dev-postgres: ## Stop development PostgreSQL services
	@echo "Stopping development PostgreSQL services..."
	docker compose -f $(COMPOSE_DEV_POSTGRES) down

.PHONY: down-dev-sqlite
down-dev-sqlite: ## Stop development SQLite services
	@echo "Stopping development SQLite services..."
	docker compose -f $(COMPOSE_DEV_SQLITE) down

.PHONY: down-cluster-postgres
down-cluster-postgres: ## Stop cluster mode PostgreSQL services
	@echo "Stopping cluster mode PostgreSQL services..."
	docker compose -f $(COMPOSE_CLUSTER_POSTGRES) down

.PHONY: down-cluster-sqlite
down-cluster-sqlite: ## Stop cluster mode SQLite services
	@echo "Stopping cluster mode SQLite services..."
	docker compose -f $(COMPOSE_CLUSTER_SQLITE) down

.PHONY: down-postgres
down-postgres: ## Stop PostgreSQL services
	@echo "Stopping PostgreSQL services..."
	docker compose -f $(COMPOSE_DEPS) --profile postgres down

.PHONY: down-sqlite
down-sqlite: ## Stop SQLite services
	@echo "Stopping SQLite services..."
	docker compose -f $(COMPOSE_DEPS) down

.PHONY: docker-down
docker-down: down-dev-$(DEFAULT_DEV_DB) ## Stop default development services

.PHONY: docker-down-all
docker-down-all: ## Stop all Docker Compose services
	@echo "Stopping all Docker services..."
	@docker compose -f $(COMPOSE_DEV_POSTGRES) down --remove-orphans 2>/dev/null || true
	@docker compose -f $(COMPOSE_DEV_SQLITE) down --remove-orphans 2>/dev/null || true
	@docker compose -f $(COMPOSE_CLUSTER_POSTGRES) down --remove-orphans 2>/dev/null || true
	@docker compose -f $(COMPOSE_CLUSTER_SQLITE) down --remove-orphans 2>/dev/null || true
	@docker compose -f $(COMPOSE_DEPS) --profile postgres down --remove-orphans 2>/dev/null || true

# Database targets
.PHONY: migrate-init
migrate-init: ## Run database migrations init
	@echo "Running database migrations init..."
	cd apps/server && go run cmd/bun/main.go db init

.PHONY: migrate-up
migrate-up: ## Run database migrations up
	@echo "Running database migrations..."
	cd apps/server && go run cmd/bun/main.go db migrate

.PHONY: migrate-down
migrate-down: ## Run database migrations down
	@echo "Rolling back database migrations..."
	cd apps/server && go run cmd/bun/main.go db rollback


# Quick database environment switchers
.PHONY: switch-to-postgres
switch-to-postgres: docker-down-all docker-dev-postgres ## Switch to PostgreSQL development environment
	@echo "Switched to PostgreSQL development environment"

.PHONY: switch-to-sqlite
switch-to-sqlite: docker-down-all docker-dev-sqlite ## Switch to SQLite development environment
	@echo "Switched to SQLite development environment"

.PHONY: test-server
test-server: ## Test the server
	@echo "Testing the server..."
	cd apps/server && go test -v ./internal/...

.PHONY: lint-web
lint-web: ## Test the web
	@echo "Testing the web..."
	cd apps/web && bun run lint && bun run build

.PHONY: sync-skills
sync-skills: ## Link shared agent skills into Claude Code
	./scripts/sync-skills.sh

.PHONY: setup
setup: ## Check that Go, Node.js and Bun are installed
	@./scripts/check-tools.sh

.PHONY: install
install: ## Install all dependencies (bun install + go mod tidy)
	@echo "📦 Installing all project dependencies..."
	@echo "Installing Node.js dependencies..."
	bun install
	@echo "Tidying Go modules..."
	cd apps/server && go mod tidy
	@echo "✅ All dependencies installed successfully!"

# The api stores the results and runs the watchdog, and a probe checks the monitors: it needs
# PEEKAPING_URL and PROBE_TOKEN, which .env.dev.example sets.
.PHONY: dev
dev: ## Start development environment
	bun run dev
