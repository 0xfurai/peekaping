# Makefile for Peekaping project
#
# This Makefile supports multiple Docker Compose configurations:
# - Development: dev-postgres, dev-mongo, dev-sqlite
# - Production:  prod-postgres, prod-mongo, prod-sqlite
# - Standalone:  postgres, mongo
#
# To change the default database, modify DEFAULT_DEV_DB or DEFAULT_PROD_DB below
# Example: make dev (uses default) vs make dev-mongo (specific database)

# Variables
GO_SERVER_DIR = apps/server
WEB_DIR = apps/web
BINARY_NAME = peekaping-server

# Docker Compose configurations
COMPOSE_DEV_POSTGRES = docker-compose.dev.postgres.yml
COMPOSE_DEV_MONGO = docker-compose.dev.mongo.yml
COMPOSE_DEV_SQLITE = docker-compose.dev.sqlite.yml
COMPOSE_PROD_POSTGRES = docker-compose.prod.postgres.yml
COMPOSE_PROD_MONGO = docker-compose.prod.mongo.yml
COMPOSE_PROD_SQLITE = docker-compose.prod.sqlite.yml
COMPOSE_POSTGRES = docker-compose.postgres.yml
COMPOSE_MONGO = docker-compose.mongo.yml

# Default configurations
DEFAULT_DEV_DB = mongo
DEFAULT_PROD_DB = mongo

# Default target
.DEFAULT_GOAL := help

# Help target - shows available commands
.PHONY: help
help: ## Show this help message
	@echo "🐳 DOCKER CONFIGURATIONS QUICK REFERENCE:"
	@echo "  \033[32mDevelopment:\033[0m   dev-postgres, dev-mongo, dev-sqlite"
	@echo "  \033[33mProduction:\033[0m    prod-postgres, prod-mongo, prod-sqlite"
	@echo "  \033[36mStandalone:\033[0m    postgres, mongo"
	@echo "  \033[35mSwitchers:\033[0m     switch-to-postgres, switch-to-mongo, switch-to-sqlite"
	@echo "  \033[31mStop All:\033[0m      docker-down-all"
	@echo ""
	@echo "📋 Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2}'

.PHONY: docker-configs
docker-configs: ## Show all available Docker Compose configurations
	@echo "📋 Available Docker Compose Configurations:"
	@echo ""
	@echo "🔧 \033[32mDEVELOPMENT ENVIRONMENTS:\033[0m"
	@echo "  make dev-postgres    # $(COMPOSE_DEV_POSTGRES)"
	@echo "  make dev-mongo       # $(COMPOSE_DEV_MONGO)"
	@echo "  make dev-sqlite      # $(COMPOSE_DEV_SQLITE)"
	@echo ""
	@echo "🚀 \033[33mPRODUCTION ENVIRONMENTS:\033[0m"
	@echo "  make prod-postgres   # $(COMPOSE_PROD_POSTGRES)"
	@echo "  make prod-mongo      # $(COMPOSE_PROD_MONGO)"
	@echo "  make prod-sqlite     # $(COMPOSE_PROD_SQLITE)"
	@echo ""
	@echo "🎯 \033[36mSTANDALONE ENVIRONMENTS:\033[0m"
	@echo "  make postgres        # $(COMPOSE_POSTGRES)"
	@echo "  make mongo           # $(COMPOSE_MONGO)"
	@echo ""
	@echo "⚡ \033[35mQUICK SWITCHERS:\033[0m"
	@echo "  make switch-to-postgres   # Stop all → Start PostgreSQL dev"
	@echo "  make switch-to-mongo      # Stop all → Start MongoDB dev"
	@echo "  make switch-to-sqlite     # Stop all → Start SQLite dev"
	@echo ""
	@echo "🔍 \033[34mUTILITY COMMANDS:\033[0m"
	@echo "  make docker-status        # Show status of all configurations"
	@echo "  make docker-ps            # Show running containers"
	@echo "  make docker-down-all      # Stop all configurations"


# Docker targets - Development Environment
.PHONY: docker-dev-postgres
docker-dev-postgres: ## Start development environment with PostgreSQL
	@echo "Starting development environment with PostgreSQL..."
	docker-compose -f $(COMPOSE_DEV_POSTGRES) up -d --build

.PHONY: docker-dev-mongo
docker-dev-mongo: ## Start development environment with MongoDB
	@echo "Starting development environment with MongoDB..."
	docker-compose -f $(COMPOSE_DEV_MONGO) up -d --build

.PHONY: docker-dev-sqlite
docker-dev-sqlite: ## Start development environment with SQLite
	@echo "Starting development environment with SQLite..."
	docker-compose -f $(COMPOSE_DEV_SQLITE) up -d --build


# Docker targets - Production Environment
.PHONY: docker-prod-postgres
docker-prod-postgres: ## Start production environment with PostgreSQL
	@echo "Starting production environment with PostgreSQL..."
	docker-compose -f $(COMPOSE_PROD_POSTGRES) up -d

.PHONY: docker-prod-mongo
docker-prod-mongo: ## Start production environment with MongoDB
	@echo "Starting production environment with MongoDB..."
	docker-compose -f $(COMPOSE_PROD_MONGO) up -d

.PHONY: docker-prod-sqlite
docker-prod-sqlite: ## Start production environment with SQLite
	@echo "Starting production environment with SQLite..."
	docker-compose -f $(COMPOSE_PROD_SQLITE) up -d


# Docker targets - Standard Configurations
.PHONY: docker-postgres
docker-postgres: ## Start PostgreSQL environment
	@echo "Starting PostgreSQL environment..."
	docker-compose -f $(COMPOSE_POSTGRES) up -d

.PHONY: docker-mongo
docker-mongo: ## Start MongoDB environment
	@echo "Starting MongoDB environment..."
	docker-compose -f $(COMPOSE_MONGO) up -d

# Docker targets - Service Management
.PHONY: down-dev-postgres
down-dev-postgres: ## Stop development PostgreSQL services
	@echo "Stopping development PostgreSQL services..."
	docker-compose -f $(COMPOSE_DEV_POSTGRES) down

.PHONY: down-dev-mongo
down-dev-mongo: ## Stop development MongoDB services
	@echo "Stopping development MongoDB services..."
	docker-compose -f $(COMPOSE_DEV_MONGO) down

.PHONY: down-dev-sqlite
down-dev-sqlite: ## Stop development SQLite services
	@echo "Stopping development SQLite services..."
	docker-compose -f $(COMPOSE_DEV_SQLITE) down

.PHONY: down-prod-postgres
down-prod-postgres: ## Stop production PostgreSQL services
	@echo "Stopping production PostgreSQL services..."
	docker-compose -f $(COMPOSE_PROD_POSTGRES) down

.PHONY: down-prod-mongo
down-prod-mongo: ## Stop production MongoDB services
	@echo "Stopping production MongoDB services..."
	docker-compose -f $(COMPOSE_PROD_MONGO) down

.PHONY: down-prod-sqlite
down-prod-sqlite: ## Stop production SQLite services
	@echo "Stopping production SQLite services..."
	docker-compose -f $(COMPOSE_PROD_SQLITE) down

.PHONY: down-postgres
down-postgres: ## Stop PostgreSQL services
	@echo "Stopping PostgreSQL services..."
	docker-compose -f $(COMPOSE_POSTGRES) down

.PHONY: down-mongo
down-mongo: ## Stop MongoDB services
	@echo "Stopping MongoDB services..."
	docker-compose -f $(COMPOSE_MONGO) down

.PHONY: docker-down
docker-down: down-dev-$(DEFAULT_DEV_DB) ## Stop default development services

.PHONY: docker-down-all
docker-down-all: ## Stop all Docker Compose services
	@echo "Stopping all Docker services..."
	@docker-compose -f $(COMPOSE_DEV_POSTGRES) down 2>/dev/null || true
	@docker-compose -f $(COMPOSE_DEV_MONGO) down 2>/dev/null || true
	@docker-compose -f $(COMPOSE_DEV_SQLITE) down 2>/dev/null || true
	@docker-compose -f $(COMPOSE_PROD_POSTGRES) down 2>/dev/null || true
	@docker-compose -f $(COMPOSE_PROD_MONGO) down 2>/dev/null || true
	@docker-compose -f $(COMPOSE_PROD_SQLITE) down 2>/dev/null || true
	@docker-compose -f $(COMPOSE_POSTGRES) down 2>/dev/null || true
	@docker-compose -f $(COMPOSE_MONGO) down 2>/dev/null || true

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
switch-to-postgres: docker-down-all dev-postgres ## Switch to PostgreSQL development environment
	@echo "Switched to PostgreSQL development environment"

.PHONY: switch-to-mongo
switch-to-mongo: docker-down-all dev-mongo ## Switch to MongoDB development environment
	@echo "Switched to MongoDB development environment"

.PHONY: switch-to-sqlite
switch-to-sqlite: docker-down-all dev-sqlite ## Switch to SQLite development environment
	@echo "Switched to SQLite development environment"

.PHONY: test-server
test-server: ## Test the server
	@echo "Testing the server..."
	cd apps/server && go test -v ./src/...

.PHONY: test-web
test-web: ## Test the web
	@echo "Testing the web..."
	cd apps/web && npm run test
