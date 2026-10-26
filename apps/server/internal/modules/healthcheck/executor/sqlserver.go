//go:build !slim

package executor

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"peekaping/internal/modules/shared"
	"regexp"
	"strconv"
	"strings"
	"time"

	mssql "github.com/denisenkom/go-mssqldb" // Microsoft SQL Server driver
	"github.com/denisenkom/go-mssqldb/msdsn"
	"go.uber.org/zap"
)

type SQLServerConfig struct {
	DatabaseConnectionString string `json:"database_connection_string" validate:"required" example:"Server=localhost,1433;Database=master;User Id=sa;Password=password;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30"`
	DatabaseQuery            string `json:"database_query" validate:"omitempty" example:"SELECT 1"`
}

type SQLServerExecutor struct {
	logger *zap.SugaredLogger
}

func NewSQLServerExecutor(logger *zap.SugaredLogger) *SQLServerExecutor {
	return &SQLServerExecutor{
		logger: logger,
	}
}

func init() { registerOptional("sqlserver", NewSQLServerExecutor) }

func (s *SQLServerExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[SQLServerConfig](configJSON)
}

// sqlServerServerValue is what the Server parameter may hold: a host, and a port.
var sqlServerServerValue = regexp.MustCompile(`^[^;,]+(,\d+)?$`)

// isSemicolonFormat reports whether the string is the Server=...;Database=...
// form: it starts with the Server, Database, User Id and Password parameters,
// in that order, and the first three have a value. A value in braces may hold
// a ";".
func isSemicolonFormat(connectionString string) bool {
	pairs := splitADOConnectionString(connectionString)
	if len(pairs) < 4 {
		return false
	}
	for i, key := range []string{"Server", "Database", "User Id", "Password"} {
		if !strings.EqualFold(pairs[i].key, key) {
			return false
		}
	}
	return sqlServerServerValue.MatchString(pairs[0].value) && pairs[1].value != "" && pairs[2].value != ""
}

func (s *SQLServerExecutor) validateConnectionString(connectionString string) error {
	if connectionString == "" {
		return fmt.Errorf("connection string cannot be empty")
	}

	// Check if it's the new semicolon-separated format
	if isSemicolonFormat(connectionString) {
		return s.validateSemicolonFormat(connectionString)
	}

	// Check if it's the legacy URL format (for backward compatibility)
	if strings.HasPrefix(connectionString, "sqlserver://") || strings.HasPrefix(connectionString, "mssql://") {
		return s.validateLegacyURL(connectionString)
	}

	return fmt.Errorf("invalid connection string format. Expected: Server=hostname,port;Database=database;User Id=username;Password=password;...")
}

func (s *SQLServerExecutor) validateSemicolonFormat(connectionString string) error {
	params := s.parseConnectionStringParams(connectionString)

	// Validate required parameters
	if params["Server"] == "" {
		return fmt.Errorf("Server parameter is required")
	}
	if params["Database"] == "" {
		return fmt.Errorf("Database parameter is required")
	}
	if params["User Id"] == "" {
		return fmt.Errorf("User Id parameter is required")
	}

	// Validate server format (can include port)
	serverParts := strings.Split(params["Server"], ",")
	if len(serverParts) > 2 {
		return fmt.Errorf("invalid Server format. Expected: hostname or hostname,port")
	}
	if len(serverParts) == 2 {
		port, err := strconv.Atoi(serverParts[1])
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("invalid port number: %s", serverParts[1])
		}
	}

	// Validate boolean parameters if present
	if encrypt := params["Encrypt"]; encrypt != "" {
		if !isValidBooleanString(encrypt) {
			return fmt.Errorf("invalid Encrypt value: %s. Expected: true or false", encrypt)
		}
	}
	if trustCert := params["TrustServerCertificate"]; trustCert != "" {
		if !isValidBooleanString(trustCert) {
			return fmt.Errorf("invalid TrustServerCertificate value: %s. Expected: true or false", trustCert)
		}
	}

	// Validate Connection Timeout if present
	if timeout := params["Connection Timeout"]; timeout != "" {
		timeoutVal, err := strconv.Atoi(timeout)
		if err != nil || timeoutVal < 0 {
			return fmt.Errorf("invalid Connection Timeout value: %s. Expected: positive integer", timeout)
		}
	}

	return nil
}

func isValidBooleanString(value string) bool {
	lower := strings.ToLower(value)
	return lower == "true" || lower == "false" || lower == "yes" || lower == "no"
}

func (s *SQLServerExecutor) parseConnectionStringParams(connectionString string) map[string]string {
	params := make(map[string]string)
	for _, pair := range splitADOConnectionString(connectionString) {
		// Normalize key to expected case
		params[s.normalizeParameterKey(pair.key)] = pair.value
	}
	return params
}

// validateLegacyURL validates SQL Server legacy URL format that uses query parameters for database
func (s *SQLServerExecutor) validateLegacyURL(connectionString string) error {
	if connectionString == "" {
		return fmt.Errorf("connection string cannot be empty")
	}

	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return fmt.Errorf("invalid connection string format: %w", err)
	}

	// Check if it's a sqlserver:// or mssql:// URL
	if parsedURL.Scheme != "sqlserver" && parsedURL.Scheme != "mssql" {
		return fmt.Errorf("invalid scheme: %s, expected sqlserver:// or mssql://", parsedURL.Scheme)
	}

	if parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return fmt.Errorf("connection string must include host")
	}

	// Username is required for SQL Server
	if parsedURL.User == nil {
		return fmt.Errorf("connection string must include username")
	}

	if parsedURL.User.Username() == "" {
		return fmt.Errorf("connection string must include username")
	}

	// For SQL Server legacy URLs, database can be in query parameters
	queryParams := parsedURL.Query()
	database := queryParams.Get("database")

	// Check if database is in path or query parameters
	hasPathDatabase := parsedURL.Path != "" && parsedURL.Path != "/"
	hasQueryDatabase := database != ""

	if !hasPathDatabase && !hasQueryDatabase {
		return fmt.Errorf("connection string must include database name (in path or ?database= parameter)")
	}

	// Validate port if provided
	if port := parsedURL.Port(); port != "" {
		if port == "0" {
			return fmt.Errorf("invalid port: 0")
		}
	}

	return nil
}

// normalizeParameterKey normalizes parameter keys to the expected standard case
func (s *SQLServerExecutor) normalizeParameterKey(key string) string {
	lowerKey := strings.ToLower(key)

	// Map of lowercase keys to their standard case equivalents
	keyMap := map[string]string{
		"server":                 "Server",
		"database":               "Database",
		"user id":                "User Id",
		"password":               "Password",
		"encrypt":                "Encrypt",
		"trustservercertificate": "TrustServerCertificate",
		"connection timeout":     "Connection Timeout",
		"timeout":                "Connection Timeout", // Alternative name
		"applicationintent":      "ApplicationIntent",
		"applicationname":        "ApplicationName",
		"workstationid":          "WorkstationID",
		"failoverpartner":        "FailoverPartner",
		"packetsizeoffset":       "PacketSizeOffset",
		"readonlyintent":         "ReadOnlyIntent",
	}

	if normalizedKey, exists := keyMap[lowerKey]; exists {
		return normalizedKey
	}

	// If not in our map, return the original key (for unknown parameters)
	return key
}

func (s *SQLServerExecutor) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}

	sqlServerCfg := cfg.(*SQLServerConfig)

	if err := s.validateConnectionString(sqlServerCfg.DatabaseConnectionString); err != nil {
		return fmt.Errorf("invalid database connection string: %w", err)
	}

	if sqlServerCfg.DatabaseQuery != "" {
		if err := s.validateQuery(sqlServerCfg.DatabaseQuery); err != nil {
			return fmt.Errorf("invalid query: %w", err)
		}
	}

	return GenericValidator(sqlServerCfg)
}

func (s *SQLServerExecutor) validateQuery(query string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	// Convert to uppercase for checking
	upperQuery := strings.ToUpper(query)

	// List of allowed query prefixes
	allowedPrefixes := []string{
		"SELECT",
		"SHOW",
		"DESCRIBE", "DESC",
		"EXPLAIN",
		"WITH",
		"VALUES",
	}

	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upperQuery, prefix) {
			return nil
		}
	}

	return fmt.Errorf("query must start with one of: %s", strings.Join(allowedPrefixes, ", "))
}

func (s *SQLServerExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := s.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*SQLServerConfig)

	startTime := time.Now().UTC()

	// Validate configuration before execution
	if err := s.validateConnectionString(cfg.DatabaseConnectionString); err != nil {
		return DownResult(fmt.Errorf("connection string validation failed: %w", err), startTime, time.Now().UTC())
	}

	// Validate the query before connecting: a query that is refused anyway
	// doesn't need a server to say so.
	query := cfg.DatabaseQuery
	if strings.TrimSpace(query) == "" {
		query = "SELECT 1"
	} else if err := s.validateQuery(query); err != nil {
		return DownResult(fmt.Errorf("query validation failed: %w", err), startTime, time.Now().UTC())
	}

	// Parse and validate connection string
	dsn, err := s.parseConnectionString(cfg.DatabaseConnectionString)
	if err != nil {
		return DownResult(fmt.Errorf("failed to parse connection string: %w", err), startTime, time.Now().UTC())
	}

	// Set connection timeout using the monitor's configured timeout
	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.Timeout)*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()

	// Open connection
	connector, err := newSQLServerConnector(dsn, deadline)
	if err != nil {
		return DownResult(fmt.Errorf("failed to open SQL Server connection: %w", err), startTime, time.Now().UTC())
	}
	db := sql.OpenDB(connector)
	defer db.Close()

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		return DownResult(fmt.Errorf("connection failed: %w", err), startTime, time.Now().UTC())
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return DownResult(fmt.Errorf("query execution failed: %w", err), startTime, time.Now().UTC())
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return DownResult(fmt.Errorf("failed to get columns: %w", err), startTime, time.Now().UTC())
	}

	// Count rows
	rowCount := 0
	for rows.Next() {
		rowCount++
		// We only need to count, don't need to scan values
	}

	if err := rows.Err(); err != nil {
		return DownResult(fmt.Errorf("error iterating rows: %w", err), startTime, time.Now().UTC())
	}

	endTime := time.Now().UTC()
	ping := endTime.Sub(startTime).Milliseconds()

	s.logger.Infof("SQL Server query successful: %s, ping: %dms", m.Name, ping)
	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   fmt.Sprintf("Query successful, ping: %dms, columns: %d, rows: %d", ping, len(columns), rowCount),
		StartTime: startTime,
		EndTime:   endTime,
	}
}

// newSQLServerConnector returns the driver's connector for the DSN, with a
// dialer whose connections can't outlive the check's deadline. The driver
// stops dialing at the dial context's deadline (which its Dial Timeout limits)
// and stops a query at the query context's, but it reads the login handshake
// without one: a server that accepts the connection and never answers would
// keep the check waiting forever. The deadline is the check's own, not the dial
// context's, which is shorter and would cut a slow query short.
func newSQLServerConnector(dsn string, deadline time.Time) (*mssql.Connector, error) {
	cfg, _, err := msdsn.Parse(dsn)
	if err != nil {
		return nil, err
	}
	connector, err := mssql.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	connector.Dialer = deadlineDialer{dialer: net.Dialer{KeepAlive: cfg.KeepAlive}, deadline: deadline}
	return connector, nil
}

// deadlineDialer dials like the driver's own dialer, and returns connections
// that can't outlive the deadline.
type deadlineDialer struct {
	dialer   net.Dialer
	deadline time.Time
}

func (d deadlineDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := d.dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(d.deadline); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return deadlineConn{Conn: conn, deadline: d.deadline}, nil
}

// deadlineConn keeps the deadlines that the driver sets (it moves them with
// every read and write when the connection string has a Connection Timeout)
// from reaching past the deadline.
type deadlineConn struct {
	net.Conn
	deadline time.Time
}

func (c deadlineConn) limit(t time.Time) time.Time {
	if t.IsZero() || t.After(c.deadline) {
		return c.deadline
	}
	return t
}

func (c deadlineConn) SetDeadline(t time.Time) error      { return c.Conn.SetDeadline(c.limit(t)) }
func (c deadlineConn) SetReadDeadline(t time.Time) error  { return c.Conn.SetReadDeadline(c.limit(t)) }
func (c deadlineConn) SetWriteDeadline(t time.Time) error { return c.Conn.SetWriteDeadline(c.limit(t)) }

func (s *SQLServerExecutor) parseConnectionString(connectionString string) (string, error) {
	// Check if it's the new semicolon-separated format
	if isSemicolonFormat(connectionString) {
		pairs := splitADOConnectionString(connectionString)
		for _, pair := range pairs {
			if pair.braced {
				// The driver reads this form by splitting at every ";", so it
				// can't carry a value that holds one. Hand it the same values
				// as a URL, which it parses without that problem.
				return sqlServerTargetFromADO(pairs).dsn(), nil
			}
		}
		// Nothing to escape: the driver reads the string as it is.
		return connectionString, nil
	}

	// Handle legacy URL format for backward compatibility
	if strings.HasPrefix(connectionString, "sqlserver://") || strings.HasPrefix(connectionString, "mssql://") {
		return s.parseURLConnectionString(connectionString)
	}

	return "", fmt.Errorf("unsupported connection string format")
}

// adoPair is one key=value of a Server=...;Database=... connection string.
type adoPair struct {
	key, value string
	braced     bool // the value was written in braces
}

// splitADOConnectionString splits a connection string at its semicolons, like
// ADO.NET does: a value in braces ends at the closing brace, may hold ";" and
// "=", and writes "}" as "}}". A value that starts with "{" but isn't a
// complete braced value (no closing brace, or text after it) is an ordinary
// value, as it was before braces were read.
func splitADOConnectionString(connectionString string) []adoPair {
	var pairs []adoPair
	rest := connectionString
	for rest != "" {
		eq, semi := strings.IndexByte(rest, '='), strings.IndexByte(rest, ';')
		if semi >= 0 && (eq < 0 || semi < eq) { // a key without a value
			rest = rest[semi+1:]
			continue
		}
		if eq < 0 {
			break
		}
		key := strings.TrimSpace(rest[:eq])
		var pair adoPair
		pair.value, pair.braced, rest = readADOValue(rest[eq+1:])
		if key != "" {
			pair.key = key
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

// readADOValue reads the value at the start of s, and returns what follows its
// semicolon.
func readADOValue(s string) (value string, braced bool, rest string) {
	if trimmed := strings.TrimLeft(s, " \t"); strings.HasPrefix(trimmed, "{") {
		var b strings.Builder
		for i := 1; i < len(trimmed); i++ {
			if trimmed[i] != '}' {
				b.WriteByte(trimmed[i])
				continue
			}
			if i+1 < len(trimmed) && trimmed[i+1] == '}' { // an escaped "}"
				b.WriteByte('}')
				i++
				continue
			}
			after := strings.TrimLeft(trimmed[i+1:], " \t")
			if after == "" {
				return b.String(), true, ""
			}
			if after[0] == ';' {
				return b.String(), true, after[1:]
			}
			break // text after the closing brace: not a braced value
		}
	}
	if semi := strings.IndexByte(s, ';'); semi >= 0 {
		return strings.TrimSpace(s[:semi]), false, s[semi+1:]
	}
	return strings.TrimSpace(s), false, ""
}

// adoSynonyms are the keys the driver reads as another key when it reads the
// Server=... form.
var adoSynonyms = map[string]string{
	"application name": "app name",
	"data source":      "server",
	"address":          "server",
	"network address":  "server",
	"addr":             "server",
	"user":             "user id",
	"uid":              "user id",
	"initial catalog":  "database",
}

// sqlServerTarget is what a connection string says, whichever form it was
// written in.
type sqlServerTarget struct {
	host, port, instance string
	user, password       string
	database             string
	options              map[string]string // the other parameters, with the driver's lowercase names
}

// dsn writes the target as the sqlserver:// URL the driver reads, which
// escapes every value on its own.
func (t sqlServerTarget) dsn() string {
	query := url.Values{}
	for key, value := range t.options {
		query.Set(key, value)
	}
	if t.database != "" {
		query.Set("database", t.database)
	}

	host, port := t.host, t.port
	if port == "" && strings.Contains(host, ":") {
		// An IPv6 address needs its brackets in a URL, and the driver reads a
		// bracketed host without a port as the host name "[::1]". With the
		// port it splits them properly; this is the port it uses anyway.
		port = "1433"
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	}
	u := url.URL{Scheme: "sqlserver", Host: host, RawQuery: query.Encode()}
	if t.instance != "" {
		u.Path = "/" + t.instance
	}
	switch {
	case t.password != "":
		u.User = url.UserPassword(t.user, t.password)
	case t.user != "":
		u.User = url.User(t.user)
	}
	return u.String()
}

func sqlServerTargetFromADO(pairs []adoPair) sqlServerTarget {
	t := sqlServerTarget{options: map[string]string{}}
	for _, pair := range pairs {
		key := strings.ToLower(pair.key)
		if synonym, ok := adoSynonyms[key]; ok {
			key = synonym
		}
		switch key {
		case "server":
			server := strings.TrimPrefix(pair.value, "tcp:")
			if name, port, ok := strings.Cut(server, ","); ok && port != "" && !strings.Contains(port, ",") {
				server, t.port = name, port
			}
			t.host, t.instance, _ = strings.Cut(server, `\`)
		case "user id":
			t.user = pair.value
		case "password":
			t.password = pair.value
		case "database":
			t.database = pair.value
		default:
			t.options[key] = pair.value
		}
	}
	return t
}

func (s *SQLServerExecutor) parseURLConnectionString(connectionString string) (string, error) {
	// Parse the URL
	parsedURL, err := url.Parse(connectionString)
	if err != nil {
		return "", fmt.Errorf("invalid connection string format: %w", err)
	}

	// Check if it's a sqlserver:// or mssql:// URL
	if parsedURL.Scheme != "sqlserver" && parsedURL.Scheme != "mssql" {
		return "", fmt.Errorf("invalid scheme: %s, expected sqlserver:// or mssql://", parsedURL.Scheme)
	}

	t := sqlServerTarget{
		host:    parsedURL.Hostname(),
		port:    parsedURL.Port(),
		options: map[string]string{},
	}
	if parsedURL.User != nil {
		t.user = parsedURL.User.Username()
		t.password, _ = parsedURL.User.Password()
	}

	// Extract database from query parameters first, then from path if not found
	queryParams := parsedURL.Query()
	t.database = queryParams.Get("database")
	if t.database == "" {
		// Try to get database from path
		if parsedURL.Path != "" && parsedURL.Path != "/" {
			t.database = strings.TrimPrefix(parsedURL.Path, "/")
		}
	}

	// Add other query parameters, except those the URL's own parts already set
	for key, values := range queryParams {
		if len(values) == 0 {
			continue
		}
		key = strings.ToLower(key)
		if synonym, ok := adoSynonyms[key]; ok {
			key = synonym
		}
		if key == "timeout" { // the name this form has always accepted
			key = "connection timeout"
		}
		switch {
		case key == "database",
			key == "server" && t.host != "",
			key == "user id" && t.user != "",
			key == "password" && t.password != "":
		default:
			t.options[key] = values[0]
		}
	}

	return t.dsn(), nil
}
