#!/bin/bash

# SMTP Health Check Test Script
# This script helps test the SMTP implementation

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "🧪 Peekaping SMTP Health Check Test Utility"
echo "============================================"
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to check if Docker is running
check_docker() {
    if ! docker info > /dev/null 2>&1; then
        echo -e "${RED}❌ Docker is not running. Please start Docker first.${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Docker is running${NC}"
}

# Function to start SMTP test server
start_servers() {
    echo ""
    echo "🚀 Starting Postfix test server..."
    cd "$SCRIPT_DIR"
    docker-compose -f docker-compose.smtp-test.yml up -d postfix
    
    echo ""
    echo "⏳ Waiting for server to be ready..."
    sleep 5
    
    echo -e "${GREEN}✅ Postfix test server is running!${NC}"
    echo ""
    echo "📧 Postfix SMTP Server:"
    echo "  • Plain SMTP:  localhost:1027"
    echo "  • STARTTLS:    localhost:1028"
    echo "  • Direct TLS:  localhost:1029"
    echo "  • Auth:        testuser / testpass"
    echo "  • Realm:       test-smtp.local"
}

# Function to stop SMTP test server
stop_servers() {
    echo ""
    echo "🛑 Stopping Postfix test server..."
    cd "$SCRIPT_DIR"
    docker-compose -f docker-compose.smtp-test.yml down
    echo -e "${GREEN}✅ Server stopped${NC}"
}

# Function to show server status
show_status() {
    echo ""
    echo "📊 Postfix Test Server Status:"
    echo ""
    docker ps --filter "name=peekaping-test-postfix" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
}

# Function to test SMTP connection
test_connection() {
    local host=${1:-localhost}
    local port=${2:-1027}
    
    echo ""
    echo "🔍 Testing SMTP connection to $host:$port..."
    
    if command -v nc > /dev/null 2>&1; then
        if nc -z -w5 "$host" "$port" 2>/dev/null; then
            echo -e "${GREEN}✅ Connection successful${NC}"
        else
            echo -e "${RED}❌ Connection failed${NC}"
        fi
    else
        echo -e "${YELLOW}⚠️  netcat (nc) not found. Install it to test connections.${NC}"
    fi
}

# Function to show example configurations
show_examples() {
    echo ""
    echo "📝 Example Monitor Configurations:"
    echo ""
    echo "1️⃣  Plain SMTP (No TLS):"
    echo '   {
     "host": "localhost",
     "port": 1027,
     "use_tls": false
   }'
    echo ""
    echo "2️⃣  STARTTLS (Port 587):"
    echo '   {
     "host": "localhost",
     "port": 1028,
     "use_tls": true,
     "use_direct_tls": false,
     "ignore_tls_errors": true,
     "check_cert_expiry": true
   }'
    echo ""
    echo "3️⃣  Direct TLS/SMTPS (Port 465):"
    echo '   {
     "host": "localhost",
     "port": 1029,
     "use_tls": true,
     "use_direct_tls": true,
     "ignore_tls_errors": true,
     "check_cert_expiry": true
   }'
    echo ""
    echo "4️⃣  STARTTLS with Authentication:"
    echo '   {
     "host": "localhost",
     "port": 1028,
     "use_tls": true,
     "use_direct_tls": false,
     "ignore_tls_errors": true,
     "username": "testuser",
     "password": "testpass"
   }'
    echo ""
    echo "5️⃣  Direct TLS with Authentication:"
    echo '   {
     "host": "localhost",
     "port": 1029,
     "use_tls": true,
     "use_direct_tls": true,
     "ignore_tls_errors": true,
     "username": "testuser",
     "password": "testpass"
   }'
    echo ""
    echo "6️⃣  Open Relay Test:"
    echo '   {
     "host": "localhost",
     "port": 1027,
     "use_tls": false,
     "from_email": "test@example.com",
     "test_open_relay": true,
     "open_relay_failure": true
   }'
}

# Function to run unit tests
run_tests() {
    echo ""
    echo "🧪 Running SMTP executor unit tests..."
    cd "$SCRIPT_DIR/../../apps/server"
    go test -v ./internal/modules/healthcheck/executor -run TestSMTP
}

# Function to run integration tests
run_integration_tests() {
    echo ""
    echo "🧪 Running SMTP executor integration tests..."
    echo "⚠️  Make sure Postfix test server is running first!"
    echo ""
    cd "$SCRIPT_DIR/../../apps/server"
    POSTFIX_TESTS=1 go test -v ./internal/modules/healthcheck/executor -run TestSMTPExecutor_Execute_Postfix_Integration
}

# Function to show logs
show_logs() {
    echo ""
    echo "📋 Postfix Server Logs (Press Ctrl+C to exit):"
    echo ""
    cd "$SCRIPT_DIR"
    docker-compose -f docker-compose.smtp-test.yml logs -f postfix
}

# Main menu
show_menu() {
    echo ""
    echo "Choose an action:"
    echo "  1) Start Postfix test server"
    echo "  2) Stop Postfix test server"
    echo "  3) Show server status"
    echo "  4) Test connection"
    echo "  5) Show example configurations"
    echo "  6) Run unit tests"
    echo "  7) Run integration tests"
    echo "  8) Show server logs"
    echo "  9) Exit"
    echo ""
}

# Main script
main() {
    check_docker
    
    if [ $# -eq 0 ]; then
        # Interactive mode
        while true; do
            show_menu
            read -p "Select option (1-9): " choice
            case $choice in
                1) start_servers ;;
                2) stop_servers ;;
                3) show_status ;;
                4) test_connection ;;
                5) show_examples ;;
                6) run_tests ;;
                7) run_integration_tests ;;
                8) show_logs ;;
                9) echo "👋 Goodbye!"; exit 0 ;;
                *) echo -e "${RED}Invalid option${NC}" ;;
            esac
        done
    else
        # Command-line mode
        case "$1" in
            start) start_servers ;;
            stop) stop_servers ;;
            status) show_status ;;
            test) test_connection "$2" "$3" ;;
            examples) show_examples ;;
            logs) show_logs ;;
            run-tests) run_tests ;;
            run-integration-tests) run_integration_tests ;;
            *)
                echo "Usage: $0 {start|stop|status|test|examples|logs|run-tests|run-integration-tests}"
                echo ""
                echo "Examples:"
                echo "  $0 start                    # Start Postfix test server"
                echo "  $0 stop                     # Stop Postfix test server"
                echo "  $0 status                   # Show server status"
                echo "  $0 test localhost 1027      # Test connection (default: port 1027)"
                echo "  $0 examples                 # Show example configs"
                echo "  $0 logs                     # Show server logs"
                echo "  $0 run-tests                # Run unit tests (uses mocks)"
                echo "  $0 run-integration-tests    # Run integration tests (requires Postfix running)"
                exit 1
                ;;
        esac
    fi
}

main "$@"

