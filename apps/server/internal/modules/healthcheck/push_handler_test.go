package healthcheck

import (
	"testing"

	"peekaping/internal/modules/shared"
)

func TestParsePushStatus(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  shared.MonitorStatus
	}{
		{name: "up string", value: "up", want: shared.MonitorStatusUp},
		{name: "down string", value: "down", want: shared.MonitorStatusDown},
		{name: "case insensitive", value: "DOWN", want: shared.MonitorStatusDown},
		{name: "numeric up", value: "1", want: shared.MonitorStatusUp},
		{name: "numeric down", value: "0", want: shared.MonitorStatusDown},
		{name: "invalid defaults to up", value: "unknown", want: shared.MonitorStatusUp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePushStatus(tt.value); got != tt.want {
				t.Errorf("parsePushStatus(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestParsePushPing(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "ping", value: "500", want: 500},
		{name: "missing defaults to zero", value: "0", want: 0},
		{name: "invalid defaults to zero", value: "unknown", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePushPing(tt.value); got != tt.want {
				t.Errorf("parsePushPing(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}
