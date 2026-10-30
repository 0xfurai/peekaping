//go:build !slim

package executor

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

var mongoSchemes = []string{"mongodb", "mongodb+srv"}

// mongoConnectionString is a MongoDB connection string cut into its parts. It is read by hand
// rather than with url.Parse, which refuses the seed list of a replica set
// (mongodb://h1:27017,h2:27017/db), and rather than with the driver's parser, which looks up the
// DNS records of a mongodb+srv name. Nothing here touches the network.
type mongoConnectionString struct {
	scheme string
	// userinfo is the user and password as written, escapes included; empty when there are none
	userinfo string
	// hosts are the entries of the seed list as written, each a host with an optional port
	hosts []string
	// database is the path after the first "/", as written
	database string
	// query is the options after the "?", as written
	query    string
	hasQuery bool
}

// splitMongoConnectionString cuts the string into its parts without judging them. It reports false
// for a string with no "://".
func splitMongoConnectionString(connectionString string) (mongoConnectionString, bool) {
	scheme, rest, ok := strings.Cut(connectionString, "://")
	if !ok {
		return mongoConnectionString{}, false
	}
	// The hosts end at the database or the options. The user and password end at the last "@",
	// which they escape in themselves.
	authority, tail := rest, ""
	if end := strings.IndexAny(rest, "/?"); end >= 0 {
		authority, tail = rest[:end], rest[end:]
	}
	parsed := mongoConnectionString{scheme: scheme}
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		parsed.userinfo, authority = authority[:at], authority[at+1:]
	}
	parsed.hosts = strings.Split(authority, ",")
	path, query, hasQuery := strings.Cut(tail, "?")
	parsed.database = strings.TrimPrefix(path, "/")
	parsed.query, parsed.hasQuery = query, hasQuery
	return parsed, true
}

// validateMongoConnectionString refuses a connection string the driver would not take: a scheme
// other than mongodb and mongodb+srv, a missing or malformed host, a port that is no port, no
// database. A seed list of several hosts is valid.
func validateMongoConnectionString(connectionString string) error {
	if connectionString == "" {
		return fmt.Errorf("connection string cannot be empty")
	}
	parsed, ok := splitMongoConnectionString(connectionString)
	if !ok {
		return fmt.Errorf("connection string must start with mongodb:// or mongodb+srv://")
	}
	if !slices.Contains(mongoSchemes, parsed.scheme) {
		return fmt.Errorf("connection string must use one of %v schemes, got: %s", mongoSchemes, parsed.scheme)
	}
	if _, err := url.PathUnescape(parsed.userinfo); err != nil {
		return fmt.Errorf("invalid credentials: %w", err)
	}
	if err := validateMongoHosts(parsed); err != nil {
		return err
	}
	return validateMongoDatabase(parsed.database)
}

func validateMongoDatabase(database string) error {
	if database == "" {
		return fmt.Errorf("connection string must include database name")
	}
	if _, err := url.PathUnescape(database); err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}
	return nil
}

// validateMongoHosts checks every entry of the seed list, and that a mongodb+srv name is one host
// with no port: the driver takes the servers and their ports from the SRV record.
func validateMongoHosts(parsed mongoConnectionString) error {
	srv := parsed.scheme == "mongodb+srv"
	if srv && len(parsed.hosts) > 1 {
		return fmt.Errorf("a mongodb+srv connection string takes one host")
	}
	for _, entry := range parsed.hosts {
		hasPort, err := validateMongoHost(entry)
		if err != nil {
			return err
		}
		if srv && hasPort {
			return fmt.Errorf("a mongodb+srv connection string takes no port")
		}
	}
	return nil
}

// validateMongoHost checks one entry of the seed list: a name, an IPv4 address or an IPv6 address
// in brackets, with an optional port, or an escaped Unix socket path. It reports whether the
// entry has a port.
func validateMongoHost(entry string) (hasPort bool, err error) {
	if strings.ContainsAny(entry, " \t\r\n") {
		return false, fmt.Errorf("invalid host %s: it holds a space", entry)
	}
	decoded, err := url.PathUnescape(entry)
	if err != nil {
		return false, fmt.Errorf("invalid host %s: %w", entry, err)
	}
	if strings.HasSuffix(decoded, ".sock") {
		return false, nil
	}
	host, port, hasPort, err := splitMongoHostPort(entry)
	if err != nil {
		return false, err
	}
	if host == "" {
		return false, fmt.Errorf("connection string must include host")
	}
	if hasPort && !validPort(port) {
		return false, fmt.Errorf("invalid port %q for host %s", port, host)
	}
	return hasPort, nil
}

// splitMongoHostPort cuts an entry of the seed list into its host and port. An IPv6 address is
// written in brackets, since its colons would be taken for the port's.
func splitMongoHostPort(entry string) (host, port string, hasPort bool, err error) {
	if !strings.HasPrefix(entry, "[") {
		if strings.Count(entry, ":") > 1 {
			return "", "", false, fmt.Errorf("invalid host %s: write an IPv6 address in brackets", entry)
		}
		host, port, hasPort = strings.Cut(entry, ":")
		return host, port, hasPort, nil
	}
	end := strings.Index(entry, "]")
	if end < 0 {
		return "", "", false, fmt.Errorf("invalid host %s: missing ]", entry)
	}
	host, after := entry[1:end], entry[end+1:]
	if after == "" {
		return host, "", false, nil
	}
	if !strings.HasPrefix(after, ":") {
		return "", "", false, fmt.Errorf("invalid host %s: text after ]", entry)
	}
	return host, after[1:], true, nil
}

// validPort is true for a decimal number from 1 to 65535
func validPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535 && strings.Trim(port, "0123456789") == ""
}
