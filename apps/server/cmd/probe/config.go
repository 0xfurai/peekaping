package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"peekaping/internal/config"

	"go.uber.org/zap/zapcore"
)

// Config is what the probe reads from the environment. It has no database or Redis: the probe
// talks to the Peekaping server over HTTP and keeps nothing on disk.
type Config struct {
	// URL is the address of the Peekaping server, such as https://peekaping.example.com
	URL string `env:"PEEKAPING_URL"`
	// Token is the probe's token, which the server finds the probe by
	Token string `env:"PROBE_TOKEN"`
	// AllowInsecure lets URL be a plain http:// address to a host that isn't this machine
	AllowInsecure bool `env:"PROBE_ALLOW_INSECURE"`
	// Concurrency is how many checks run at the same time
	Concurrency int `env:"PROBE_CONCURRENCY" default:"128"`
	// ReadyAddr is where the /ready endpoint listens
	ReadyAddr string `env:"PROBE_READY_ADDR" default:"127.0.0.1:8081"`

	Mode     string `env:"MODE" default:"prod"`
	LogLevel string `env:"LOG_LEVEL" default:"info"`
}

// The checks below are written out instead of being validate tags, because the validator names a
// field by its Go name ("URL is required"), and a person who forgot to set a variable needs its name.

// LoadAndValidate loads the probe's configuration and checks it. Its error names each variable
// that is missing or wrong, one to a line.
func LoadAndValidate(path string) (*Config, error) {
	cfg, err := config.LoadConfig[Config](path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	// Validation reads the URL and the token trimmed, so the probe runs with them trimmed too
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.Token = strings.TrimSpace(cfg.Token)
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks the probe's configuration
func Validate(cfg *Config) error {
	var problems []error
	if strings.TrimSpace(cfg.URL) == "" {
		problems = append(problems, errors.New("PEEKAPING_URL is required: set it to the address of your Peekaping server, such as https://peekaping.example.com"))
	} else if err := validateURL(cfg); err != nil {
		problems = append(problems, err)
	}
	if strings.TrimSpace(cfg.Token) == "" {
		problems = append(problems, errors.New("PROBE_TOKEN is required: create the probe in Peekaping and set this to its token"))
	}
	if cfg.Concurrency < 1 {
		problems = append(problems, errors.New("PROBE_CONCURRENCY must be at least 1"))
	}
	if err := validateAddr(cfg.ReadyAddr); err != nil {
		problems = append(problems, err)
	}
	if !slices.Contains([]string{"dev", "prod", "test"}, cfg.Mode) {
		problems = append(problems, errors.New("MODE must be one of: dev, prod, test"))
	}
	if _, err := parseLogLevel(cfg.LogLevel); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

// validateURL checks that URL is the address of a server, and that the probe's token is not sent
// over plain http to a machine that isn't this one, unless the admin allowed it
func validateURL(cfg *Config) error {
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		// The value isn't repeated: it may hold a password
		return errors.New("PEEKAPING_URL must be an address starting with https://, such as https://peekaping.example.com")
	}
	if u.Scheme == "http" && !cfg.AllowInsecure && !isLoopback(u.Hostname()) {
		return fmt.Errorf("PEEKAPING_URL is a plain http:// address to %s, which would send the probe's token unencrypted: "+
			"use https://, or set PROBE_ALLOW_INSECURE=true to allow it", u.Hostname())
	}
	return nil
}

// isLoopback reports whether host is this machine: localhost, 127.0.0.1 or ::1
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.Equal(net.IPv4(127, 0, 0, 1)) || ip.Equal(net.IPv6loopback))
}

// validateAddr checks that addr is a host:port the ready endpoint can listen on. The host may be empty.
func validateAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err == nil {
		var n int
		if n, err = strconv.Atoi(port); err == nil && (n < 1 || n > 65535) {
			err = errors.New("the port is out of range")
		}
	}
	if err != nil {
		return fmt.Errorf("PROBE_READY_ADDR must be a host and port, such as 127.0.0.1:8081 (%w)", err)
	}
	return nil
}

// parseLogLevel reads LOG_LEVEL, which takes what the other binaries take
func parseLogLevel(level string) (zapcore.Level, error) {
	name := strings.ToLower(level)
	if name == "warning" {
		name = "warn"
	}
	var parsed zapcore.Level
	if err := parsed.UnmarshalText([]byte(name)); err != nil {
		return zapcore.InfoLevel, errors.New("LOG_LEVEL must be one of: debug, info, warn, warning, error, dpanic, panic, fatal")
	}
	return parsed, nil
}
