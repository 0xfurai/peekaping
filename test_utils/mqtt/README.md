# MQTT Test Broker

This directory holds a Mosquitto broker and a small publisher for testing the MQTT monitor.

## What it starts

`docker-compose.mqtt.yml` starts two containers:

- `mqtt-broker`: Eclipse Mosquitto 2.0.18. MQTT is on port 1883 and MQTT over WebSockets on port 9001. It allows anonymous connections, so no username or password is needed. The settings are in `mqtt-config/mosquitto.conf`, and the broker writes its data to `mqtt-data/` and its logs to `mqtt-logs/` here (both are ignored by git).
- `mqtt-publisher`: waits 10 seconds, then publishes a message to each of `test/topic`, `sensor/status` and `sensor/error`, and publishes to `test/topic` and `sensor/status` again every 30 seconds.

| Topic           | Message                                         |
| --------------- | ----------------------------------------------- |
| `test/topic`    | `Hello World - success`                         |
| `sensor/status` | `{"status": "healthy", "temperature": 25.5}`    |
| `sensor/error`  | `{"status": "error", "code": 500}`              |

The later messages on `test/topic` add the date to the text, and the ones on `sensor/status` have a random temperature from 20.0 to 30.9.

The messages are not retained (the publisher doesn't use `-r` and `mosquitto.conf` doesn't retain them). A monitor that subscribes sees `test/topic` and `sensor/status` only when the next 30-second message arrives, and `sensor/error` only once, at startup.

## Start and stop

From the repository root:

```bash
docker compose -f test_utils/mqtt/docker-compose.mqtt.yml up -d
docker compose -f test_utils/mqtt/docker-compose.mqtt.yml down
```

## Use it with an MQTT monitor

Create an MQTT monitor with host `localhost`, port `1883` and one of the topics above, for example `test/topic` with the keyword `success`. If Peekaping runs in a container, use an address that container can reach instead of `localhost`.

## Publish more messages

`mqtt-test-scripts/test-mqtt.sh` publishes more messages to the broker on `localhost:1883`, such as messages that should fail a keyword or JSON query check. It needs `mosquitto_pub` on your machine (the `mosquitto-clients` package, or `brew install mosquitto`).

```bash
./test_utils/mqtt/mqtt-test-scripts/test-mqtt.sh
```
