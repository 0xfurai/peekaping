#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.rabbitmq.yml"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_usage() {
    echo "Usage: $0 {start|stop|restart|status|logs|test}"
    echo ""
    echo "Commands:"
    echo "  start    - Start RabbitMQ containers"
    echo "  stop     - Stop RabbitMQ containers"
    echo "  restart  - Restart RabbitMQ containers"
    echo "  status   - Show container status"
    echo "  logs     - Show container logs"
    echo "  test     - Test RabbitMQ API endpoints"
    echo ""
    echo "Configuration for your monitor:"
    echo "  Nodes: [\"http://localhost:15672\", \"http://localhost:15673\", \"http://localhost:15674\"]"
    echo "  Username: admin"
    echo "  Password: password"
}

start_rabbitmq() {
    echo -e "${GREEN}Starting RabbitMQ containers...${NC}"
    docker-compose -f "$COMPOSE_FILE" up -d

    echo -e "${YELLOW}Waiting for RabbitMQ to be ready...${NC}"
    sleep 10

    echo -e "${GREEN}RabbitMQ Management UIs available at:${NC}"
    echo "  Node 1: http://localhost:15672 (admin/password)"
    echo "  Node 2: http://localhost:15673 (admin/password)"
    echo "  Node 3: http://localhost:15674 (admin/password)"
    echo ""
    echo -e "${GREEN}Health check endpoints:${NC}"
    echo "  Node 1: http://localhost:15672/api/health/checks/alarms/"
    echo "  Node 2: http://localhost:15673/api/health/checks/alarms/"
    echo "  Node 3: http://localhost:15674/api/health/checks/alarms/"
}

stop_rabbitmq() {
    echo -e "${RED}Stopping RabbitMQ containers...${NC}"
    docker-compose -f "$COMPOSE_FILE" down
}

restart_rabbitmq() {
    stop_rabbitmq
    sleep 2
    start_rabbitmq
}

show_status() {
    echo -e "${GREEN}RabbitMQ Container Status:${NC}"
    docker-compose -f "$COMPOSE_FILE" ps
}

show_logs() {
    echo -e "${GREEN}RabbitMQ Container Logs:${NC}"
    docker-compose -f "$COMPOSE_FILE" logs -f
}

test_endpoints() {
    echo -e "${GREEN}Testing RabbitMQ API endpoints...${NC}"

    for port in 15672 15673 15674; do
        echo -e "\n${YELLOW}Testing Node on port $port:${NC}"

        # Test basic connectivity
        if curl -s -u admin:password "http://localhost:$port/api/overview" > /dev/null; then
            echo -e "${GREEN}✓ API accessible${NC}"
        else
            echo -e "${RED}✗ API not accessible${NC}"
            continue
        fi

        # Test health endpoint (the one your monitor uses)
        response=$(curl -s -w "%{http_code}" -u admin:password "http://localhost:$port/api/health/checks/alarms/")
        http_code=${response: -3}

        if [ "$http_code" = "200" ]; then
            echo -e "${GREEN}✓ Health check passed (HTTP $http_code)${NC}"
        elif [ "$http_code" = "503" ]; then
            echo -e "${YELLOW}! Health check failed (HTTP $http_code) - This is normal for testing failure scenarios${NC}"
        else
            echo -e "${RED}✗ Unexpected response (HTTP $http_code)${NC}"
        fi
    done

    echo -e "\n${GREEN}Monitor Configuration:${NC}"
    echo '{'
    echo '  "nodes": ["http://localhost:15672", "http://localhost:15673", "http://localhost:15674"],'
    echo '  "username": "admin",'
    echo '  "password": "password"'
    echo '}'
}

case "$1" in
    start)
        start_rabbitmq
        ;;
    stop)
        stop_rabbitmq
        ;;
    restart)
        restart_rabbitmq
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs
        ;;
    test)
        test_endpoints
        ;;
    *)
        print_usage
        exit 1
        ;;
esac
