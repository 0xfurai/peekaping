-- Create custom database
CREATE DATABASE TestDB;
GO

-- Use the custom database
USE TestDB;
GO

-- Create a test table
CREATE TABLE Users (
    ID INT IDENTITY(1,1) PRIMARY KEY,
    Name NVARCHAR(100) NOT NULL,
    Email NVARCHAR(255) NOT NULL,
    CreatedAt DATETIME2 DEFAULT GETDATE()
);
GO

-- Insert some test data
INSERT INTO Users (Name, Email) VALUES
    ('John Doe', 'john.doe@example.com'),
    ('Jane Smith', 'jane.smith@example.com'),
    ('Bob Johnson', 'bob.johnson@example.com');
GO

-- Create another test table
CREATE TABLE Products (
    ID INT IDENTITY(1,1) PRIMARY KEY,
    Name NVARCHAR(100) NOT NULL,
    Price DECIMAL(10,2) NOT NULL,
    Category NVARCHAR(50),
    CreatedAt DATETIME2 DEFAULT GETDATE()
);
GO

-- Insert test product data
INSERT INTO Products (Name, Price, Category) VALUES
    ('Laptop', 999.99, 'Electronics'),
    ('Mouse', 29.99, 'Electronics'),
    ('Desk', 199.99, 'Furniture'),
    ('Chair', 149.99, 'Furniture');
GO

-- Create a view for testing
CREATE VIEW UserSummary AS
SELECT
    COUNT(*) as TotalUsers,
    MAX(CreatedAt) as LastUserCreated
FROM Users;
GO

-- Create a stored procedure for testing
CREATE PROCEDURE GetUserCount
AS
BEGIN
    SELECT COUNT(*) as UserCount FROM Users;
END
GO
