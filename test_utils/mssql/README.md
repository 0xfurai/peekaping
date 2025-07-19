# Microsoft SQL Server Test Containers

This directory contains Docker Compose configurations for testing the Microsoft SQL Server monitor with various scenarios.

## ✅ Status

**WORKING**: SQL Server 2019 containers are running successfully on ARM64 (Apple Silicon) and x86_64 systems.

## Available Containers

### 1. Standard SQL Server (`mssql`)
- **Port**: 1433
- **Username**: sa
- **Password**: TestPassword123!
- **Connection String**: `Server=localhost,1433;Database=master;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30`

### 2. SQL Server with Custom Database (`mssql-custom-db`)
- **Port**: 1434
- **Username**: sa
- **Password**: TestPassword123!
- **Database**: TestDB (with sample tables and data)
- **Connection String**: `Server=localhost,1434;Database=TestDB;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30`

### 3. SQL Server with Custom Credentials (`mssql-custom-creds`)
- **Port**: 1435
- **Username**: sa
- **Password**: CustomPassword456!
- **Connection String**: `Server=localhost,1435;Database=master;User Id=sa;Password=CustomPassword456!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30`

### 4. SQL Server with Custom User (`mssql-custom-user`)
- **Port**: 1436
- **Username**: TestUser
- **Password**: TestUserPass123!
- **Database**: UserTestDB
- **Connection String**: `Server=localhost,1436;Database=UserTestDB;User Id=TestUser;Password=TestUserPass123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30`

## Getting Started

### Quick Start
```bash
cd test_utils/mssql

# Check your system compatibility
./check-docker-resources.sh

# Start all SQL Server containers
./start-mssql.sh

# Test connections (wait 2-3 minutes after starting)
./test-connection.sh
```

### Manual Commands

#### 1. Start all containers
```bash
cd test_utils/mssql
docker-compose -f docker-compose.mssql-test.yml up -d
```

#### 2. Start specific container
```bash
# Start only the standard SQL Server
docker-compose -f docker-compose.mssql-test.yml up -d mssql

# Start only the custom database container
docker-compose -f docker-compose.mssql-test.yml up -d mssql-custom-db
```

#### 3. Check container status
```bash
docker-compose -f docker-compose.mssql-test.yml ps
```

#### 4. View logs
```bash
# View all logs
docker-compose -f docker-compose.mssql-test.yml logs

# View specific container logs
docker-compose -f docker-compose.mssql-test.yml logs mssql
```

#### 5. Stop containers
```bash
docker-compose -f docker-compose.mssql-test.yml down
```

## Test Queries

### Standard SQL Server
```sql
-- Basic connectivity test
SELECT 1 as test_value;

-- System information
SELECT @@VERSION as version;

-- Database list
SELECT name FROM sys.databases;
```

### Custom Database (TestDB)
```sql
-- Use the custom database
USE TestDB;

-- Query the Users table
SELECT * FROM Users;

-- Query the Products table
SELECT * FROM Products;

-- Use the view
SELECT * FROM UserSummary;

-- Execute the stored procedure
EXEC GetUserCount;
```

### Custom User Database (UserTestDB)
```sql
-- Use the custom database
USE UserTestDB;

-- Query the test data
SELECT * FROM TestData;

-- Use the view
SELECT * FROM TestDataView;

-- Execute the stored procedure
EXEC GetTestData;
```

## Monitor Configuration Examples

### Standard SQL Server Monitor
```json
{
  "name": "SQL Server Test",
  "type": "mssql",
  "config": {
    "connectionString": "Server=localhost,1433;Database=master;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30",
    "query": "SELECT 1 as test_value"
  }
}
```

### Custom Database Monitor
```json
{
  "name": "SQL Server Custom DB Test",
  "type": "mssql",
  "config": {
    "connectionString": "Server=localhost,1434;Database=TestDB;User Id=sa;Password=TestPassword123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30",
    "query": "SELECT COUNT(*) as user_count FROM Users"
  }
}
```

### Custom User Monitor
```json
{
  "name": "SQL Server Custom User Test",
  "type": "mssql",
  "config": {
    "connectionString": "Server=localhost,1436;Database=UserTestDB;User Id=TestUser;Password=TestUserPass123!;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30",
    "query": "SELECT COUNT(*) as data_count FROM TestData"
  }
}
```

## Connection String Parameters

The SQL Server connection string uses the following format:
```
Server=<hostname>,<port>;Database=<database>;User Id=<username>;Password=<password>;Encrypt=<true/false>;TrustServerCertificate=<true/false>;Connection Timeout=<seconds>
```

### Parameter Details:
- **Server**: Hostname and port (comma-separated)
- **Database**: Database name to connect to
- **User Id**: Username for authentication
- **Password**: Password for authentication
- **Encrypt**: Whether to use SSL encryption (false for local testing)
- **TrustServerCertificate**: Trust self-signed certificates (true for local testing)
- **Connection Timeout**: Connection timeout in seconds (30 is recommended)

## Troubleshooting

### ✅ Containers are working but showing "unhealthy"
This is normal. The health check uses a simple port check. As long as the containers stay running and ports are accessible, SQL Server is working correctly.

### Container won't start
1. **Check Docker memory**: Ensure Docker has at least 2GB RAM allocated (4GB recommended)
2. **Check architecture**: On Apple Silicon, containers run in emulation mode (slower but functional)
3. **Check ports**: Ensure ports 1433-1436 aren't in use by other services
4. **Check logs**: `docker logs <container-name>` for detailed error messages

### Connection issues
1. **Wait for initialization**: SQL Server needs 2-3 minutes to fully start
2. **Verify ports are open**: Use `nc -z localhost 1433` to test connectivity
3. **Check container status**: Use `docker ps` to ensure containers are running
4. **Verify connection strings**: Double-check username, password, and database names

### Permission issues
1. **For custom user scenarios**: Ensure the user has proper permissions
2. **Check if the database exists**: Custom databases are created during container initialization
3. **Volume issues**: If persistent problems, remove volumes: `docker-compose down -v`

### ARM64 (Apple Silicon) Specific
- Containers run in emulation mode (linux/amd64 on arm64)
- Startup is slower but functionality is identical
- Some features may have reduced performance

## System Requirements

- **Docker**: 20.10+
- **Memory**: 2GB minimum, 4GB recommended
- **Disk**: 2GB free space
- **Ports**: 1433-1436 available

## Cleanup

To remove all containers and volumes:
```bash
docker-compose -f docker-compose.mssql-test.yml down -v
```

To remove only containers (keep volumes):
```bash
docker-compose -f docker-compose.mssql-test.yml down
```

## Scripts

- **`start-mssql.sh`**: Start all containers with status reporting
- **`test-connection.sh`**: Test connections to all containers
- **`check-docker-resources.sh`**: Check system compatibility and resources
