#!/bin/bash

# Check Docker resources and system compatibility for SQL Server
# This script helps troubleshoot SQL Server container issues

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔍 Docker Resources and SQL Server Compatibility Check${NC}"
echo "============================================================"

# Check if Docker is running
echo -e "\n${YELLOW}📦 Docker Status:${NC}"
if docker info >/dev/null 2>&1; then
    echo -e "${GREEN}✅ Docker is running${NC}"
else
    echo -e "${RED}❌ Docker is not running${NC}"
    exit 1
fi

# Check system architecture
echo -e "\n${YELLOW}🏗️  System Architecture:${NC}"
ARCH=$(uname -m)
echo "Host architecture: $ARCH"

if [[ "$ARCH" == "arm64" ]]; then
    echo -e "${YELLOW}⚠️  You're on Apple Silicon (ARM64)${NC}"
    echo "SQL Server containers will run in emulation mode (slower but functional)"
else
    echo -e "${GREEN}✅ Native x86_64 architecture${NC}"
fi

# Check Docker memory settings
echo -e "\n${YELLOW}💾 Docker Memory Settings:${NC}"
DOCKER_MEMORY=$(docker system info --format '{{.MemTotal}}' 2>/dev/null)
if [[ -n "$DOCKER_MEMORY" ]]; then
    MEMORY_GB=$((DOCKER_MEMORY / 1024 / 1024 / 1024))
    echo "Docker memory limit: ${MEMORY_GB}GB"

    if [[ $MEMORY_GB -ge 4 ]]; then
        echo -e "${GREEN}✅ Sufficient memory for SQL Server (${MEMORY_GB}GB >= 4GB)${NC}"
    elif [[ $MEMORY_GB -ge 2 ]]; then
        echo -e "${YELLOW}⚠️  Minimal memory for SQL Server (${MEMORY_GB}GB, recommended: 4GB+)${NC}"
    else
        echo -e "${RED}❌ Insufficient memory for SQL Server (${MEMORY_GB}GB < 2GB minimum)${NC}"
        echo -e "${YELLOW}💡 Increase Docker memory in Docker Desktop Settings${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  Unable to determine Docker memory limit${NC}"
fi

# Check available disk space
echo -e "\n${YELLOW}💿 Disk Space:${NC}"
AVAILABLE_SPACE=$(df -h . | awk 'NR==2 {print $4}')
echo "Available disk space: $AVAILABLE_SPACE"

# Check for existing SQL Server containers
echo -e "\n${YELLOW}🐳 Existing SQL Server Containers:${NC}"
EXISTING_CONTAINERS=$(docker ps -a --filter "name=peekaping-mssql" --format "{{.Names}}")
if [[ -n "$EXISTING_CONTAINERS" ]]; then
    echo "Found existing containers:"
    docker ps -a --filter "name=peekaping-mssql" --format "table {{.Names}}\t{{.Status}}\t{{.Image}}"
else
    echo -e "${GREEN}✅ No existing SQL Server containers${NC}"
fi

# Check for existing volumes
echo -e "\n${YELLOW}📚 Existing SQL Server Volumes:${NC}"
EXISTING_VOLUMES=$(docker volume ls --filter "name=mssql" --format "{{.Name}}")
if [[ -n "$EXISTING_VOLUMES" ]]; then
    echo "Found existing volumes:"
    docker volume ls --filter "name=mssql"
    echo -e "${YELLOW}💡 Consider removing volumes if having issues: docker volume prune${NC}"
else
    echo -e "${GREEN}✅ No existing SQL Server volumes${NC}"
fi

# Check port availability
echo -e "\n${YELLOW}🔌 Port Availability:${NC}"
PORTS=(1433 1434 1435 1436)
for port in "${PORTS[@]}"; do
    if lsof -i :$port >/dev/null 2>&1; then
        echo -e "${RED}❌ Port $port is in use${NC}"
    else
        echo -e "${GREEN}✅ Port $port is available${NC}"
    fi
done

# Recommendations
echo -e "\n${BLUE}💡 Recommendations:${NC}"
echo "1. Ensure Docker has at least 4GB memory allocated"
echo "2. On Apple Silicon, containers will be slower due to emulation"
echo "3. Allow 2-3 minutes for SQL Server to fully start"
echo "4. If containers keep restarting, check logs: docker logs <container-name>"

echo -e "\n${GREEN}🚀 Ready to start SQL Server containers!${NC}"
