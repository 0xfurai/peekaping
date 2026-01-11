#!/bin/bash
cd /Users/marcon/Projects/OpenSource/vigi/apps/server/internal/modules

# NotificationChannel
echo "=== NotificationChannel ==="
# Model
sed -i '' '/ID.*string.*json:"id"/a\
OrgID     string    `json:"org_id"`
' notification_channel/notification_channel.model.go

# Service
sed -i '' 's/FindByID(ctx context.Context, id string) (\*Model, error)/FindByID(ctx context.Context, id string, orgID string) (*Model, error)/' notification_channel/notification_channel.service.go
sed -i '' 's/FindAll(ctx context.Context, page int, limit int, q string) (\[\]\*Model, error)/FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error)/' notification_channel/notification_channel.service.go
sed -i '' 's/func (s \*ServiceImpl) FindByID(ctx context.Context, id string)/func (s *ServiceImpl) FindByID(ctx context.Context, id string, orgID string)/' notification_channel/notification_channel.service.go
sed -i '' 's/func (s \*ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string)/func (s *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' notification_channel/notification_channel.service.go
sed -i '' 's/s\.repository\.FindByID(ctx, id)/s.repository.FindByID(ctx, id, orgID)/' notification_channel/notification_channel.service.go
sed -i '' 's/s\.repository\.FindAll(ctx, page, limit, q)/s.repository.FindAll(ctx, page, limit, q, orgID)/' notification_channel/notification_channel.service.go

# SQL Repo
sed -i '' 's/func (r \*SQLRepositoryImpl) FindByID(ctx context.Context, id string)/func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string, orgID string)/' notification_channel/notification_channel.sql.repository.go
sed -i '' 's/func (r \*SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string)/func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' notification_channel/notification_channel.sql.repository.go

# Mongo Repo
sed -i '' 's/func (r \*MongoRepositoryImpl) FindByID(ctx context.Context, id string)/func (r *MongoRepositoryImpl) FindByID(ctx context.Context, id string, orgID string)/' notification_channel/notification_channel.mongo.repository.go
sed -i '' 's/func (r \*MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string)/func (r *MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' notification_channel/notification_channel.mongo.repository.go

# Tag
echo "=== Tag ==="
sed -i '' '/ID.*string.*json:"id"/a\
OrgID     string    `json:"org_id"`
' tag/tag.model.go

sed -i '' 's/FindByID(ctx context.Context, id string) (\*Model, error)/FindByID(ctx context.Context, id string, orgID string) (*Model, error)/' tag/tag.service.go
sed -i '' 's/FindAll(ctx context.Context, page int, limit int, q string) (\[\]\*Model, error)/FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error)/' tag/tag.service.go
sed -i '' 's/func (s \*ServiceImpl) FindByID(ctx context.Context, id string)/func (s *ServiceImpl) FindByID(ctx context.Context, id string, orgID string)/' tag/tag.service.go
sed -i '' 's/func (s \*ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string)/func (s *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' tag/tag.service.go
sed -i '' 's/s\.repository\.FindByID(ctx, id)/s.repository.FindByID(ctx, id, orgID)/' tag/tag.service.go
sed -i '' 's/s\.repository\.FindAll(ctx, page, limit, q)/s.repository.FindAll(ctx, page, limit, q, orgID)/' tag/tag.service.go

sed -i '' 's/func (r \*SQLRepositoryImpl) FindByID(ctx context.Context, id string)/func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string, orgID string)/' tag/tag.sql.repository.go
sed -i '' 's/func (r \*SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string)/func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' tag/tag.sql.repository.go

sed -i '' 's/func (r \*MongoRepositoryImpl) FindByID(ctx context.Context, id string)/func (r *MongoRepositoryImpl) FindByID(ctx context.Context, id string, orgID string)/' tag/tag.mongo.repository.go
sed -i '' 's/func (r \*MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string)/func (r *MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' tag/tag.mongo.repository.go

# StatusPage
echo "=== StatusPage ==="
sed -i '' '/ID.*string.*json:"id"/a\
OrgID     string    `json:"org_id"`
' status_page/status_page.model.go

sed -i '' 's/FindByID(ctx context.Context, id string) (\*Model, error)/FindByID(ctx context.Context, id string, orgID string) (*Model, error)/' status_page/status_page.service.go
sed -i '' 's/FindAll(ctx context.Context, page int, limit int, q string) (\[\]\*Model, error)/FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error)/' status_page/status_page.service.go
sed -i '' 's/func (s \*ServiceImpl) FindByID(ctx context.Context, id string)/func (s *ServiceImpl) FindByID(ctx context.Context, id string, orgID string)/' status_page/status_page.service.go
sed -i '' 's/func (s \*ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string)/func (s *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' status_page/status_page.service.go
sed -i '' 's/s\.repository\.FindByID(ctx, id)/s.repository.FindByID(ctx, id, orgID)/' status_page/status_page.service.go
sed -i '' 's/s\.repository\.FindAll(ctx, page, limit, q)/s.repository.FindAll(ctx, page, limit, q, orgID)/' status_page/status_page.service.go

sed -i '' 's/func (r \*SQLRepositoryImpl) FindByID(ctx context.Context, id string)/func (r *SQLRepositoryImpl) FindByID(ctx context.Context, id string, orgID string)/' status_page/status_page.sql.repository.go
sed -i '' 's/func (r \*SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string)/func (r *SQLRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' status_page/status_page.sql.repository.go

sed -i '' 's/func (r \*MongoRepositoryImpl) FindByID(ctx context.Context, id string)/func (r *MongoRepositoryImpl) FindByID(ctx context.Context, id string, orgID string)/' status_page/status_page.mongo.repository.go
sed -i '' 's/func (r \*MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string)/func (r *MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' status_page/status_page.mongo.repository.go

echo "✓ All modules updated!"
