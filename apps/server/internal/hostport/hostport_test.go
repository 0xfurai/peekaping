package hostport_test

import (
	"testing"

	"peekaping/internal/hostport"

	"github.com/stretchr/testify/assert"
)

func TestBare(t *testing.T) {
	cases := []struct {
		name string
		host string
		want string
	}{
		{"hostname", "example.com", "example.com"},
		{"IPv4", "192.0.2.1", "192.0.2.1"},
		{"bare IPv6", "::1", "::1"},
		{"bracketed IPv6", "[::1]", "::1"},
		{"bracketed IPv6 with a zone", "[fe80::1%eth0]", "fe80::1%eth0"},
		{"only one pair is removed", "[[::1]]", "[::1]"},
		{"opening bracket only", "[::1", "[::1"},
		{"closing bracket only", "::1]", "::1]"},
		{"a single bracket", "[", "["},
		{"empty brackets", "[]", "[]"},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hostport.Bare(tc.host))
		})
	}
}

func TestJoin(t *testing.T) {
	cases := []struct {
		name string
		host string
		port int
		want string
	}{
		{"hostname", "example.com", 25, "example.com:25"},
		{"IPv4", "192.0.2.1", 8080, "192.0.2.1:8080"},
		{"bare IPv6", "::1", 587, "[::1]:587"},
		{"bracketed IPv6", "[::1]", 587, "[::1]:587"},
		{"bracketed IPv6 with a zone", "[fe80::1%eth0]", 587, "[fe80::1%eth0]:587"},
		{"empty brackets are left as they are", "[]", 25, "[]:25"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hostport.Join(tc.host, tc.port))
		})
	}
}
