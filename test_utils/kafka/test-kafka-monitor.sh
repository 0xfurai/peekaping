#!/bin/bash

# Test script for Kafka Producer Monitor
# This script helps you test the kafka-producer monitor with the Docker Compose setup

set -e

echo "🐳 Kafka Producer Monitor Test Setup"
echo "===================================="

# Function to check if Docker is running
check_docker() {
    if ! docker info > /dev/null 2>&1; then
        echo "❌ Docker is not running. Please start Docker and try again."
        exit 1
    fi
}

# Function to check if Docker Compose is available
check_docker_compose() {
    if ! command -v docker-compose &> /dev/null; then
        echo "❌ Docker Compose is not installed. Please install Docker Compose and try again."
        exit 1
    fi
}

# Function to start Kafka stack
start_kafka() {
    echo "🚀 Starting Kafka stack..."
    docker-compose -f docker-compose.kafka.yml up -d

    echo "⏳ Waiting for Kafka to be ready..."
    sleep 30

    echo "✅ Kafka stack is running!"
    echo "   - Kafka: localhost:9092"
    echo "   - Zookeeper: localhost:2181"
    echo "   - Kafka UI: http://localhost:8081"
}

# Function to create test topic
create_test_topic() {
    echo "📝 Creating test topic 'monitor-test'..."
    docker exec kafka-tools kafka-topics --create \
        --bootstrap-server kafka:29092 \
        --replication-factor 1 \
        --partitions 1 \
        --topic monitor-test

    echo "✅ Topic 'monitor-test' created successfully!"
}

# Function to list topics
list_topics() {
    echo "📋 Listing topics..."
    docker exec kafka-tools kafka-topics --list \
        --bootstrap-server kafka:29092
}

# Function to consume messages from test topic
consume_messages() {
    echo "👂 Consuming messages from 'monitor-test' topic..."
    echo "Press Ctrl+C to stop consuming"
    docker exec kafka-tools kafka-console-consumer \
        --bootstrap-server kafka:29092 \
        --topic monitor-test \
        --from-beginning
}

# Function to stop Kafka stack
stop_kafka() {
    echo "🛑 Stopping Kafka stack..."
    docker-compose -f docker-compose.kafka.yml down
    echo "✅ Kafka stack stopped!"
}

# Function to show monitor configuration
show_monitor_config() {
    echo ""
    echo "📋 Kafka Producer Monitor Configuration Example:"
    echo "================================================"
    echo "Monitor Type: kafka-producer"
    echo "Brokers: [\"localhost:9092\"]"
    echo "Topic: monitor-test"
    echo "Message: {\"status\": \"up\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"monitor\": \"kafka-producer-test\"}"
    echo "Allow Auto Topic Creation: false"
    echo "SSL: false"
    echo "SASL Mechanism: None"
    echo ""
    echo "🌐 Kafka UI: http://localhost:8081"
    echo "   - Use this to view topics and messages"
    echo ""
    echo "🔧 Test Commands:"
    echo "   - List topics: ./test-kafka-monitor.sh list"
    echo "   - Consume messages: ./test-kafka-monitor.sh consume"
    echo "   - Stop Kafka: ./test-kafka-monitor.sh stop"
}

# Function to show help
show_help() {
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  start     - Start Kafka stack"
    echo "  stop      - Stop Kafka stack"
    echo "  create    - Create test topic"
    echo "  list      - List all topics"
    echo "  consume   - Consume messages from test topic"
    echo "  config    - Show monitor configuration example"
    echo "  help      - Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 start    # Start Kafka stack"
    echo "  $0 config   # Show monitor configuration"
    echo "  $0 consume  # Watch for messages"
}

# Main script logic
case "${1:-help}" in
    start)
        check_docker
        check_docker_compose
        start_kafka
        create_test_topic
        show_monitor_config
        ;;
    stop)
        stop_kafka
        ;;
    create)
        create_test_topic
        ;;
    list)
        list_topics
        ;;
    consume)
        consume_messages
        ;;
    config)
        show_monitor_config
        ;;
    help|*)
        show_help
        ;;
esac
