# MySQL Monitor Type Implementation

This document describes the implementation of the MySQL/MariaDB monitor type for the Peekaping monitoring system.

## Overview

The MySQL monitor type allows monitoring of MySQL and MariaDB databases by executing SQL queries and measuring response times. It follows the existing pattern established by other monitor types in the system.

## Backend Implementation

### Files Created/Modified

1. **`apps/server/src/modules/healthcheck/executor/mysql.go`** - MySQL executor implementation
2. **`apps/server/src/modules/healthcheck/executor/mysql_test.go`** - Tests for MySQL executor
3. **`apps/server/src/modules/healthcheck/executor/executor.go`** - Added MySQL executor to registry

### MySQL Executor Features

- **Connection String**: Uses standard MySQL connection string format (`user:password@tcp(host:port)/database`)
- **Query Execution**: Executes configurable SQL queries (defaults to `SELECT 1`)
- **Password Support**: Optional separate password field for additional security
- **Row Counting**: Returns the number of rows returned by the query
- **Error Handling**: Comprehensive error handling for connection and query failures
- **Timeout Support**: Uses context for timeout handling

### Configuration Schema

```go
type MySQLConfig struct {
    ConnectionString string `json:"connection_string" validate:"required"`
    Query           string `json:"query" validate:"required"`
    Password        string `json:"password"`
}
```

### Usage Example

```json
{
  "connection_string": "user:password@tcp(localhost:3306)/mydb",
  "query": "SELECT COUNT(*) FROM users",
  "password": "optional_password"
}
```

## Frontend Implementation

### Files Created/Modified

1. **`apps/web/src/app/monitors/components/mysql/index.tsx`** - MySQL form component
2. **`apps/web/src/app/monitors/components/shared/general.tsx`** - Added MySQL to monitor types
3. **`apps/web/src/app/monitors/components/monitor-registry.ts`** - Registered MySQL component
4. **`apps/web/src/app/monitors/context/monitor-form-context.tsx`** - Added MySQL to form schema

### Form Fields

- **Connection String**: Input field for MySQL connection string
- **Password**: Optional password field (type="password")
- **Query**: Textarea for SQL query input

### Form Schema

```typescript
export const mysqlSchema = z.object({
  type: z.literal("mysql"),
  connection_string: z.string().min(1, "Connection string is required"),
  query: z.string().min(1, "Query is required"),
  password: z.string().optional(),
})
```

## Testing

### Backend Tests

- **Validation Tests**: Verify configuration validation works correctly
- **Unmarshal Tests**: Test JSON parsing of configuration
- **Execute Tests**: Test monitor execution (structure validation)
- **Error Handling Tests**: Test invalid configuration handling

### Running Tests

```bash
# Run MySQL executor tests
cd apps/server
go test -v ./src/modules/healthcheck/executor/ -run TestMySQLExecutor

# Run all executor tests
go test -v ./src/modules/healthcheck/executor/
```

## Integration

The MySQL monitor type is fully integrated into the system:

1. **Backend**: Registered in executor registry, available for health checks
2. **Frontend**: Available in monitor type dropdown, form validation, serialization/deserialization
3. **Database**: Uses existing monitor storage structure with JSON config

## Security Considerations

- Connection strings may contain sensitive information
- Password field is optional for cases where authentication is handled via connection string
- Input validation prevents injection attacks
- Timeout handling prevents hanging connections

## Dependencies

- **Backend**: Uses `github.com/go-sql-driver/mysql` (already included in project)
- **Frontend**: Uses existing form validation and UI components

## Future Enhancements

- SSL/TLS connection support
- Connection pooling
- Advanced query result validation
- Query performance metrics
- Support for stored procedures

## Troubleshooting

### Common Issues

1. **Connection Failures**: Check connection string format and network connectivity
2. **Query Errors**: Verify SQL syntax and permissions
3. **Timeout Issues**: Adjust timeout settings in monitor configuration

### Example Connection Strings

```
# Basic connection
user:password@tcp(localhost:3306)/database

# With specific parameters
user:password@tcp(host:3306)/database?charset=utf8&parseTime=True&loc=Local

# Unix socket
user:password@unix(/var/run/mysqld/mysqld.sock)/database
```

This implementation provides a robust and user-friendly way to monitor MySQL/MariaDB databases within the Peekaping monitoring system.