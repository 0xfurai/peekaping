package certificate_test

import (
	"testing"
	"time"

	"peekaping/internal/modules/certificate"

	"github.com/stretchr/testify/assert"
)

func TestCalculateDaysRemaining(t *testing.T) {
	tests := []struct {
		name     string
		validTo  time.Time
		expected int
	}{
		{name: "whole days ahead, partial day dropped", validTo: time.Now().Add(14*24*time.Hour + 6*time.Hour), expected: 14},
		{name: "less than a day ahead", validTo: time.Now().Add(5 * time.Hour), expected: 0},
		{name: "expired a day and a half ago", validTo: time.Now().Add(-36 * time.Hour), expected: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, certificate.CalculateDaysRemaining(tt.validTo))
		})
	}
}
