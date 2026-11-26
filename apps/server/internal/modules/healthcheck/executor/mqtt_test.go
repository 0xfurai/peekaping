package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMQTTBrokerURL(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		want     string
	}{
		{"hostname", "broker.example.com", "mqtt://broker.example.com:1883"},
		{"IPv4", "192.0.2.1", "mqtt://192.0.2.1:1883"},
		{"IPv6", "2001:db8::1", "mqtt://[2001:db8::1]:1883"},
		{"bracketed IPv6", "[2001:db8::1]", "mqtt://[2001:db8::1]:1883"},
		{"mqtt scheme", "mqtt://broker.example.com", "mqtt://broker.example.com:1883"},
		{"mqtts scheme", "mqtts://broker.example.com", "mqtts://broker.example.com:1883"},
		{"scheme with IPv6", "mqtts://2001:db8::1", "mqtts://[2001:db8::1]:1883"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mqttBrokerURL(tt.hostname, 1883))
		})
	}
}
