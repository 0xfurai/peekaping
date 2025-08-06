package notification_sent_history

import (
	"context"
	"peekaping/src/config"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID        primitive.ObjectID `bson:"_id"`
	Type      string             `bson:"type"`
	MonitorID string             `bson:"monitor_id"`
	Days      int                `bson:"days"`
	CreatedAt time.Time          `bson:"created_at"`
}

func toDomainModelFromMongo(mm *mongoModel) *Model {
	if mm == nil {
		return nil
	}
	return &Model{
		ID:        mm.ID.Hex(),
		Type:      mm.Type,
		MonitorID: mm.MonitorID,
		Days:      mm.Days,
		CreatedAt: mm.CreatedAt,
	}
}

type MongoRepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("notification_sent_history")

	// Create compound index for uniqueness and performance
	indexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "type", Value: 1},
			{Key: "monitor_id", Value: 1},
			{Key: "days", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	}
	collection.Indexes().CreateOne(context.Background(), indexModel)

	// Create index for cleanup operations
	createdAtIndex := mongo.IndexModel{
		Keys: bson.D{{Key: "created_at", Value: 1}},
	}
	collection.Indexes().CreateOne(context.Background(), createdAtIndex)

	return &MongoRepositoryImpl{client, db, collection}
}

func (r *MongoRepositoryImpl) CheckIfSent(ctx context.Context, notificationType string, monitorID string, days int) (bool, error) {
	filter := bson.M{
		"type":       notificationType,
		"monitor_id": monitorID,
		"days":       bson.M{"$gte": days},
	}

	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *MongoRepositoryImpl) RecordSent(ctx context.Context, dto *CreateDto) error {
	mm := &mongoModel{
		ID:        primitive.NewObjectID(),
		Type:      dto.Type,
		MonitorID: dto.MonitorID,
		Days:      dto.Days,
		CreatedAt: time.Now(),
	}

	// Use upsert to handle duplicates gracefully
	filter := bson.M{
		"type":       dto.Type,
		"monitor_id": dto.MonitorID,
		"days":       dto.Days,
	}

	update := bson.M{
		"$setOnInsert": bson.M{
			"_id":        mm.ID,
			"type":       mm.Type,
			"monitor_id": mm.MonitorID,
			"days":       mm.Days,
			"created_at": mm.CreatedAt,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := r.collection.UpdateOne(ctx, filter, update, opts)
	return err
}

func (r *MongoRepositoryImpl) ClearByMonitorAndType(ctx context.Context, monitorID string, notificationType string) error {
	filter := bson.M{
		"monitor_id": monitorID,
		"type":       notificationType,
	}

	_, err := r.collection.DeleteMany(ctx, filter)
	return err
}

func (r *MongoRepositoryImpl) CleanupOldRecords(ctx context.Context, olderThanDays int) error {
	cutoffDate := time.Now().AddDate(0, 0, -olderThanDays)

	filter := bson.M{
		"created_at": bson.M{"$lt": cutoffDate},
	}

	_, err := r.collection.DeleteMany(ctx, filter)
	return err
}

func (r *MongoRepositoryImpl) GetByMonitorAndType(ctx context.Context, monitorID string, notificationType string) ([]*Model, error) {
	filter := bson.M{
		"monitor_id": monitorID,
		"type":       notificationType,
	}

	opts := options.Find().SetSort(bson.D{{Key: "days", Value: 1}})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var mongoModels []*mongoModel
	if err = cursor.All(ctx, &mongoModels); err != nil {
		return nil, err
	}

	var models []*Model
	for _, mm := range mongoModels {
		models = append(models, toDomainModelFromMongo(mm))
	}

	return models, nil
}
