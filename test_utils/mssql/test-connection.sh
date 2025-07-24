#!/bin/bash

# Test script for SQL Server containers
# This script tests connections to all SQL Server containers

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Testing SQL Server containers...${NC}"

# Function to test SQL Server connection
test_mssql_connection() {
    local container_name=$1
    local port=$2
    local username=$3
    local password=$4
    local database=${5:-master}

    echo -e "\n${YELLOW}Testing $container_name on port $port...${NC}"

    # Check if container is running
    if ! docker ps | grep -q "$container_name"; then
        echo -e "${RED}Container $container_name is not running!${NC}"
        return 1
    fi

    # Test connection using sqlcmd
    if docker exec "$container_name" /opt/mssql-tools/bin/sqlcmd \
        -S localhost \
        -U "$username" \
        -P "$password" \
        -d "$database" \
        -Q "SELECT 1 as test_value, @@VERSION as version" \
        -b > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Connection to $container_name successful${NC}"
        return 0
    else
        echo -e "${RED}✗ Connection to $container_name failed${NC}"
        return 1
    fi
}

# Test all containers
echo "Starting connection tests..."

# Test standard SQL Server
test_mssql_connection "peekaping-mssql-test" "1433" "sa" "TestPassword123!" "master"

# Test custom database SQL Server
test_mssql_connection "peekaping-mssql-custom-db-test" "1434" "sa" "TestPassword123!" "TestDB"

# Test custom credentials SQL Server
test_mssql_connection "peekaping-mssql-custom-creds-test" "1435" "sa" "CustomPassword456!" "master"

# Test custom user SQL Server
test_mssql_connection "peekaping-mssql-custom-user-test" "1436" "TestUser" "TestUserPass123!" "UserTestDB"

echo -e "\n${GREEN}Connection tests completed!${NC}"

# Show container status
echo -e "\n${YELLOW}Container Status:${NC}"
docker ps --filter "name=peekaping-mssql" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

# Show connection strings for reference
echo -e "\n${YELLOW}Connection Strings for Testing:${NC}"
echo "Standard SQL Server: sqlserver://sa:TestPassword123!@localhost:1433?database=master"
echo "Custom DB SQL Server: sqlserver://sa:TestPassword123!@localhost:1434?database=TestDB"
echo "Custom Creds SQL Server: sqlserver://sa:CustomPassword456!@localhost:1435?database=master"
echo "Custom User SQL Server: sqlserver://TestUser:TestUserPass123!@localhost:1436?database=UserTestDB"
