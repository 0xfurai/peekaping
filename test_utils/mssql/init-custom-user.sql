-- Create a custom database for the user
CREATE DATABASE UserTestDB;
GO

-- Use the custom database
USE UserTestDB;
GO

-- Create a custom login
CREATE LOGIN TestUser WITH PASSWORD = 'TestUserPass123!';
GO

-- Create a user in the database
CREATE USER TestUser FOR LOGIN TestUser;
GO

-- Grant permissions to the user
GRANT SELECT, INSERT, UPDATE, DELETE ON DATABASE::UserTestDB TO TestUser;
GO

-- Create a test table
CREATE TABLE TestData (
    ID INT IDENTITY(1,1) PRIMARY KEY,
    Name NVARCHAR(100) NOT NULL,
    Value NVARCHAR(255),
    CreatedAt DATETIME2 DEFAULT GETDATE()
);
GO

-- Grant permissions on the table
GRANT SELECT, INSERT, UPDATE, DELETE ON TestData TO TestUser;
GO

-- Insert some test data
INSERT INTO TestData (Name, Value) VALUES
    ('Test Item 1', 'Value 1'),
    ('Test Item 2', 'Value 2'),
    ('Test Item 3', 'Value 3');
GO

-- Create a view for the user
CREATE VIEW TestDataView AS
SELECT ID, Name, Value, CreatedAt FROM TestData;
GO

-- Grant permissions on the view
GRANT SELECT ON TestDataView TO TestUser;
GO

-- Create a stored procedure for the user
CREATE PROCEDURE GetTestData
AS
BEGIN
    SELECT * FROM TestData;
END
GO

-- Grant execute permission on the stored procedure
GRANT EXECUTE ON GetTestData TO TestUser;
GO
