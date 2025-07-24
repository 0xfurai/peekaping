#!/bin/bash

# Start Microsoft SQL Server test containers
# This script starts all SQL Server containers for testing

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.mssql-test.yml"

echo -e "${BLUE}🚀 Starting Microsoft SQL Server test containers...${NC}"

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    echo -e "${RED}❌ Docker is not running. Please start Docker and try again.${NC}"
    exit 1
fi

# Check if compose file exists
if [ ! -f "$COMPOSE_FILE" ]; then
    echo -e "${RED}❌ Docker Compose file not found: $COMPOSE_FILE${NC}"
    exit 1
fi

# Stop any existing containers
echo -e "${YELLOW}🛑 Stopping any existing SQL Server containers...${NC}"
docker-compose -f "$COMPOSE_FILE" down --remove-orphans 2>/dev/null || true

# Start the containers
echo -e "${YELLOW}🏗️  Starting SQL Server containers...${NC}"
if docker-compose -f "$COMPOSE_FILE" up -d; then
    echo -e "${GREEN}✅ SQL Server containers started successfully!${NC}"
else
    echo -e "${RED}❌ Failed to start SQL Server containers${NC}"
    exit 1
fi

# Wait for containers to be running
echo -e "${YELLOW}⏳ Waiting for containers to start...${NC}"
sleep 10

# Check container status
echo -e "\n${BLUE}📊 Container Status:${NC}"
docker-compose -f "$COMPOSE_FILE" ps

# Show running containers
echo -e "\n${BLUE}🐳 Running SQL Server containers:${NC}"
docker ps --filter "name=peekaping-mssql" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

# Show connection information
echo -e "\n${GREEN}🔗 Connection Information:${NC}"
echo -e "${YELLOW}Standard SQL Server:${NC}"
echo "  Host: localhost"
echo "  Port: 1433"
echo "  Username: sa"
echo "  Password: TestPassword123!"
echo "  Connection String: Server=localhost,1433;Database=master;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30"

echo -e "\n${YELLOW}Custom Database SQL Server:${NC}"
echo "  Host: localhost"
echo "  Port: 1434"
echo "  Username: sa"
echo "  Password: TestPassword123!"
echo "  Database: TestDB"
echo "  Connection String: Server=localhost,1434;Database=TestDB;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30"

echo -e "\n${YELLOW}Custom Credentials SQL Server:${NC}"
echo "  Host: localhost"
echo "  Port: 1435"
echo "  Username: sa"
echo "  Password: CustomPassword456!"
echo "  Connection String: Server=localhost,1435;Database=master;User Id=sa;Password=CustomPassword456!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30"

echo -e "\n${YELLOW}Custom User SQL Server:${NC}"
echo "  Host: localhost"
echo "  Port: 1436"
echo "  Username: TestUser"
echo "  Password: TestUserPass123!"
echo "  Database: UserTestDB"
echo "  Connection String: Server=localhost,1436;Database=UserTestDB;User Id=TestUser;Password=TestUserPass123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30"

echo -e "\n${GREEN}📝 Next Steps:${NC}"
echo "1. Wait 60-90 seconds for SQL Server to fully initialize"
echo "2. Run './test-connection.sh' to test connections"
echo "3. Or use 'make test-mssql' from the project root"

echo -e "\n${BLUE}💡 Helpful Commands:${NC}"
echo "  View logs: docker-compose -f $COMPOSE_FILE logs"
echo "  Stop containers: docker-compose -f $COMPOSE_FILE down"
echo "  Test connections: ./test-connection.sh"
echo "  Monitor status: docker-compose -f $COMPOSE_FILE ps"

echo -e "\n${GREEN}✨ SQL Server containers are starting up!${NC}"
