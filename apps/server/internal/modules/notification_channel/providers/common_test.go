package providers

import (
	"testing"

	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
)

func TestMonitorAddress_TCPHost(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   string
	}{
		{"hostname", `{"host": "db.example.com", "port": 5432}`, "db.example.com:5432"},
		{"IPv4", `{"host": "192.0.2.1", "port": 5432}`, "192.0.2.1:5432"},
		{"bare IPv6", `{"host": "::1", "port": 5432}`, "[::1]:5432"},
		{"bracketed IPv6", `{"host": "[::1]", "port": 5432}`, "[::1]:5432"},
		{"bracketed IPv6 without a port", `{"host": "[::1]"}`, "[::1]"},
		{"bare IPv6 without a port", `{"host": "::1"}`, "::1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := monitorAddress(&monitor.Model{Type: "tcp", Config: tc.config})

			assert.Equal(t, tc.want, got)
		})
	}
}
