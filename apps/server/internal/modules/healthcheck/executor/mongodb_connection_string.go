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
// rather than with url.Parse, which refuses some seed lists of a replica set (the last host
// without a port, a host in brackets), and rather than with the driver's parser, which looks up
// the DNS records of a mongodb+srv name. Nothing here touches the network.
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
	// fragment is the text after the "#" that ends the string
	fragment    string
	hasFragment bool
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
	tail, parsed.fragment, parsed.hasFragment = strings.Cut(tail, "#")
	path, query, hasQuery := strings.Cut(tail, "?")
	parsed.database = strings.TrimPrefix(path, "/")
	parsed.query, parsed.hasQuery = query, hasQuery
	return parsed, true
}

// validateMongoConnectionString refuses a connection string that is malformed: a scheme other than
// mongodb and mongodb+srv, no host, a port that is no port, a broken escape, no database. A seed
// list of several hosts is valid. The errors never quote the string, which holds a password: they
// say what is wrong, and which host by its place in the list.
func validateMongoConnectionString(connectionString string) error {
	if connectionString == "" {
		return fmt.Errorf("connection string cannot be empty")
	}
	parsed, ok := splitMongoConnectionString(connectionString)
	if !ok {
		return fmt.Errorf("connection string must start with mongodb:// or mongodb+srv://")
	}
	if !slices.Contains(mongoSchemes, parsed.scheme) {
		return fmt.Errorf("connection string must use mongodb:// or mongodb+srv://")
	}
	if _, err := url.PathUnescape(parsed.userinfo); err != nil {
		return fmt.Errorf("the user name or password has a bad %%-escape")
	}
	if err := validateMongoHosts(parsed.hosts); err != nil {
		return err
	}
	return validateMongoDatabase(parsed.database)
}

func validateMongoDatabase(database string) error {
	if database == "" {
		return fmt.Errorf("connection string must include database name")
	}
	if _, err := url.PathUnescape(database); err != nil {
		return fmt.Errorf("the database name has a bad %%-escape")
	}
	return nil
}

// validateMongoHosts checks the ports and brackets of the seed list, and that it names a host. An
// empty entry (a trailing comma) and an empty port are left alone: the driver takes both.
func validateMongoHosts(entries []string) error {
	named := false
	for i, entry := range entries {
		host, port, ok := splitMongoHostPort(entry)
		if !ok {
			return fmt.Errorf("host %d has a [ without a closing ]", i+1)
		}
		if port != "" && !validMongoPort(port) {
			return fmt.Errorf("the port of host %d must be a number from 1 to 65535", i+1)
		}
		named = named || host != ""
	}
	if !named {
		return fmt.Errorf("connection string must include host")
	}
	return nil
}

// splitMongoHostPort cuts an entry of the seed list into its host and port, at the last colon. An
// IPv6 address is written in brackets. It reports false for a bracket that is not closed.
func splitMongoHostPort(entry string) (host, port string, ok bool) {
	if strings.HasPrefix(entry, "[") {
		end := strings.Index(entry, "]")
		if end < 0 {
			return "", "", false
		}
		return entry[1:end], strings.TrimPrefix(entry[end+1:], ":"), true
	}
	if colon := strings.LastIndex(entry, ":"); colon >= 0 {
		return entry[:colon], entry[colon+1:], true
	}
	return entry, "", true
}

// validMongoPort is true for a decimal number from 1 to 65535
func validMongoPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535 && strings.Trim(port, "0123456789") == ""
}

// mongoHasOption is true when the options set the option, whatever the case of its name: the
// driver reads them without regard to case
func mongoHasOption(query, name string) bool {
	options, _ := url.ParseQuery(query)
	for key, values := range options {
		if strings.EqualFold(key, name) && len(values) > 0 && values[0] != "" {
			return true
		}
	}
	return false
}
