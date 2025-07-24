#!/bin/bash

# MQTT Test Script
# This script publishes various test messages to different topics

BROKER_HOST="localhost"
BROKER_PORT="1883"

echo "MQTT Test Script"
echo "================="
echo "Broker: $BROKER_HOST:$BROKER_PORT"
echo ""

# Function to publish a message
publish_message() {
    local topic=$1
    local message=$2
    echo "Publishing to topic '$topic': $message"
    mosquitto_pub -h $BROKER_HOST -p $BROKER_PORT -t "$topic" -m "$message"
    echo ""
}

# Test 1: Keyword check - success
echo "Test 1: Keyword check (should succeed)"
publish_message "test/topic" "Hello World - success"

# Test 2: Keyword check - failure
echo "Test 2: Keyword check (should fail)"
publish_message "test/topic" "Hello World - failure"

# Test 3: JSON query check - success
echo "Test 3: JSON query check (should succeed)"
publish_message "sensor/status" '{"status": "healthy", "temperature": 25.5, "humidity": 60}'

# Test 4: JSON query check - failure
echo "Test 4: JSON query check (should fail)"
publish_message "sensor/status" '{"status": "error", "temperature": 25.5, "humidity": 60}'

# Test 5: Complex JSON
echo "Test 5: Complex JSON structure"
publish_message "device/status" '{"device": {"id": "sensor001", "status": "online", "location": "room1"}, "data": {"temperature": 23.4, "humidity": 45.2}}'

# Test 6: Array in JSON
echo "Test 6: JSON with arrays"
publish_message "sensors/data" '{"sensors": [{"id": "temp1", "value": 24.5}, {"id": "temp2", "value": 25.1}], "timestamp": "2024-01-15T10:30:00Z"}'

echo "All test messages published!"
echo ""
echo "You can now test your MQTT monitor with these topics:"
echo "- test/topic (for keyword checks)"
echo "- sensor/status (for JSON query checks)"
echo "- device/status (for complex JSON queries)"
echo "- sensors/data (for JSON with arrays)"
