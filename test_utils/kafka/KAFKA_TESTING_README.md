# Kafka Producer Monitor Testing Guide

This guide helps you test the kafka-producer monitor implementation using a local Kafka cluster.

## 🚀 Quick Start

### 1. Start Kafka Stack

```bash
./test-kafka-monitor.sh start
```

This will:
- Start Zookeeper and Kafka containers
- Create a test topic called `monitor-test`
- Show you the monitor configuration to use

### 2. Configure Kafka Producer Monitor

In your Peekaping application, create a new monitor with these settings:

**Monitor Configuration:**
- **Type**: `kafka-producer`
- **Brokers**: `["localhost:9092"]`
- **Topic**: `monitor-test`
- **Message**: `{"status": "up", "timestamp": "2024-01-01T00:00:00Z", "monitor": "kafka-producer-test"}`
- **Allow Auto Topic Creation**: `false`
- **SSL**: `false`
- **SASL Mechanism**: `None`

### 3. Test the Monitor

1. **Watch for messages** (in a separate terminal):
   ```bash
   ./test-kafka-monitor.sh consume
   ```

2. **Check Kafka UI** (optional):
   - Open http://localhost:8081 in your browser
   - Navigate to the `monitor-test` topic
   - View messages being produced by your monitor

## 📋 Available Commands

```bash
# Start Kafka stack and create test topic
./test-kafka-monitor.sh start

# Stop Kafka stack
./test-kafka-monitor.sh stop

# Create test topic manually
./test-kafka-monitor.sh create

# List all topics
./test-kafka-monitor.sh list

# Consume messages from test topic
./test-kafka-monitor.sh consume

# Show monitor configuration example
./test-kafka-monitor.sh config

# Show help
./test-kafka-monitor.sh help
```

## 🔧 Manual Docker Compose Commands

If you prefer to use Docker Compose directly:

```bash
# Start the stack
docker-compose -f docker-compose.kafka.yml up -d

# Stop the stack
docker-compose -f docker-compose.kafka.yml down

# View logs
docker-compose -f docker-compose.kafka.yml logs -f kafka

# Create topic manually
docker exec kafka kafka-topics --create \
  --bootstrap-server kafka:29092 \
  --replication-factor 1 \
  --partitions 1 \
  --topic monitor-test

# List topics
docker exec kafka kafka-topics --list \
  --bootstrap-server kafka:29092

# Consume messages
docker exec kafka kafka-console-consumer \
  --bootstrap-server kafka:29092 \
  --topic monitor-test \
  --from-beginning
```

## 🌐 Services

| Service | Port | Description |
|---------|------|-------------|
| Kafka | 9092 | Main Kafka broker |
| Zookeeper | 2181 | Kafka coordination service |
| Kafka UI | 8081 | Web interface for Kafka management |
| Kafka JMX | 9101 | JMX monitoring port |

## 📊 Testing Scenarios

### 1. Basic Connectivity Test
- Configure monitor with correct broker address
- Should produce messages successfully
- Monitor status should be "UP"

### 2. Invalid Broker Test
- Configure monitor with incorrect broker address (e.g., `localhost:9093`)
- Monitor should fail to connect
- Monitor status should be "DOWN"

### 3. Invalid Topic Test
- Configure monitor with non-existent topic
- With `allow_auto_topic_creation: false` - should fail
- With `allow_auto_topic_creation: true` - should succeed

### 4. Message Content Test
- Try different message formats (JSON, plain text)
- Verify messages appear in Kafka topic
- Check message content in Kafka UI

### 5. Security Test (Optional)
- Enable SSL/TLS
- Configure SASL authentication
- Test with secure Kafka setup

## 🐛 Troubleshooting

### Kafka not starting
```bash
# Check if ports are available
netstat -an | grep 9092
netstat -an | grep 2181

# Check Docker logs
docker-compose -f docker-compose.kafka.yml logs
```

### Monitor not connecting
1. Verify Kafka is running: `docker ps`
2. Check broker address: should be `localhost:9092`
3. Verify topic exists: `./test-kafka-monitor.sh list`
4. Check monitor logs in Peekaping application

### Messages not appearing
1. Check if monitor is active in Peekaping
2. Verify topic name is correct
3. Check Kafka UI for messages
4. Use consume command to watch for messages

## 📝 Example Monitor JSON Configuration

```json
{
  "type": "kafka-producer",
  "name": "Kafka Test Monitor",
  "brokers": ["localhost:9092"],
  "topic": "monitor-test",
  "message": "{\"status\": \"up\", \"timestamp\": \"2024-01-01T00:00:00Z\", \"monitor\": \"test\"}",
  "allow_auto_topic_creation": false,
  "ssl": false,
  "sasl_mechanism": "None",
  "sasl_username": "",
  "sasl_password": "",
  "interval": 60,
  "timeout": 16,
  "max_retries": 3,
  "retry_interval": 60,
  "resend_interval": 10,
  "notification_ids": [],
  "tag_ids": [],
  "proxy_id": ""
}
```

## 🧹 Cleanup

To completely remove the Kafka stack and data:

```bash
# Stop and remove containers
docker-compose -f docker-compose.kafka.yml down

# Remove volumes (this will delete all data)
docker-compose -f docker-compose.kafka.yml down -v

# Remove images (optional)
docker rmi confluentinc/cp-kafka:7.4.0 confluentinc/cp-zookeeper:7.4.0 provectuslabs/kafka-ui:latest
```

## 📚 Additional Resources

- [Confluent Kafka Docker Images](https://docs.confluent.io/platform/current/installation/docker/config-reference.html)
- [Kafka UI Documentation](https://github.com/provectus/kafka-ui)
- [IBM Sarama Go Client](https://github.com/IBM/sarama)
- [Kafka Producer Monitor Implementation](./kafka-producer-implementation-summary.md)
