#!/bin/sh
# wait-for-db.sh - Wait for database to be ready before starting application services
set -e

DB_TYPE="${DB_TYPE:-postgres}"
MAX_RETRIES=60
RETRY_DELAY=1

echo "Waiting for $DB_TYPE database to be ready..."

case "$DB_TYPE" in
    postgres|postgresql)
        DB_HOST="${DB_HOST:-localhost}"
        DB_PORT="${DB_PORT:-5432}"
        DB_NAME="${DB_NAME:-peekaping}"
        DB_USER="${DB_USER:-postgres}"

        for i in $(seq 1 $MAX_RETRIES); do
            if PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1" >/dev/null 2>&1; then
                echo "PostgreSQL is ready!"
                exit 0
            fi
            echo "Attempt $i/$MAX_RETRIES: PostgreSQL is not ready yet, waiting ${RETRY_DELAY}s..."
            sleep $RETRY_DELAY
        done
        echo "ERROR: PostgreSQL failed to become ready after $MAX_RETRIES attempts"
        exit 1
        ;;

    mysql)
        DB_HOST="${DB_HOST:-localhost}"
        DB_PORT="${DB_PORT:-3306}"
        DB_NAME="${DB_NAME:-peekaping}"
        DB_USER="${DB_USER:-root}"

        for i in $(seq 1 $MAX_RETRIES); do
            if mysql -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" -p"$DB_PASS" -e "SELECT 1" "$DB_NAME" >/dev/null 2>&1; then
                echo "MySQL is ready!"
                exit 0
            fi
            echo "Attempt $i/$MAX_RETRIES: MySQL is not ready yet, waiting ${RETRY_DELAY}s..."
            sleep $RETRY_DELAY
        done
        echo "ERROR: MySQL failed to become ready after $MAX_RETRIES attempts"
        exit 1
        ;;

    sqlite)
        DB_NAME="${DB_NAME:-/app/data/peekaping.db}"

        for i in $(seq 1 $MAX_RETRIES); do
            if [ -f "$DB_NAME" ] && sqlite3 "$DB_NAME" "SELECT 1" >/dev/null 2>&1; then
                echo "SQLite is ready!"
                exit 0
            fi
            echo "Attempt $i/$MAX_RETRIES: SQLite is not ready yet, waiting ${RETRY_DELAY}s..."
            sleep $RETRY_DELAY
        done
        echo "ERROR: SQLite failed to become ready after $MAX_RETRIES attempts"
        exit 1
        ;;

    mongo|mongodb)
        DB_HOST="${DB_HOST:-localhost}"
        DB_PORT="${DB_PORT:-27017}"

        for i in $(seq 1 $MAX_RETRIES); do
            if mongosh --host "$DB_HOST" --port "$DB_PORT" --eval "db.adminCommand('ping')" >/dev/null 2>&1; then
                echo "MongoDB is ready!"
                exit 0
            fi
            echo "Attempt $i/$MAX_RETRIES: MongoDB is not ready yet, waiting ${RETRY_DELAY}s..."
            sleep $RETRY_DELAY
        done
        echo "ERROR: MongoDB failed to become ready after $MAX_RETRIES attempts"
        exit 1
        ;;

    *)
        echo "ERROR: Unsupported database type: $DB_TYPE"
        exit 1
        ;;
esac

