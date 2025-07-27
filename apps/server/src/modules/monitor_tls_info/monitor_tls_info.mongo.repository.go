package monitor_tls_info

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
	MonitorID string             `bson:"monitor_id"`
	InfoJSON  string             `bson:"info_json"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

func toDomainModelFromMongo(mm *mongoModel) *Model {
	if mm == nil {
		return nil
	}
	return &Model{
		ID:        mm.ID.Hex(),
		MonitorID: mm.MonitorID,
		InfoJSON:  mm.InfoJSON,
		CreatedAt: mm.CreatedAt,
		UpdatedAt: mm.UpdatedAt,
	}
}

type MongoRepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("monitor_tls_info")

	// Create unique index for monitor_id
	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "monitor_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
	collection.Indexes().CreateOne(context.Background(), indexModel)

	// Create index for updated_at for cleanup operations
	updatedAtIndex := mongo.IndexModel{
		Keys: bson.D{{Key: "updated_at", Value: 1}},
	}
	collection.Indexes().CreateOne(context.Background(), updatedAtIndex)

	return &MongoRepositoryImpl{client, db, collection}
}

func (r *MongoRepositoryImpl) GetByMonitorID(ctx context.Context, monitorID string) (*Model, error) {
	var mm mongoModel
	err := r.collection.FindOne(ctx, bson.M{"monitor_id": monitorID}).Decode(&mm)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	return toDomainModelFromMongo(&mm), nil
}

func (r *MongoRepositoryImpl) Create(ctx context.Context, dto *CreateDto) (*Model, error) {
	mm := &mongoModel{
		ID:        primitive.NewObjectID(),
		MonitorID: dto.MonitorID,
		InfoJSON:  dto.InfoJSON,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err := r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromMongo(mm), nil
}

func (r *MongoRepositoryImpl) Update(ctx context.Context, monitorID string, dto *UpdateDto) (*Model, error) {
	filter := bson.M{"monitor_id": monitorID}
	update := bson.M{
		"$set": bson.M{
			"info_json":  dto.InfoJSON,
			"updated_at": time.Now(),
		},
	}

	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return nil, err
	}

	return r.GetByMonitorID(ctx, monitorID)
}

func (r *MongoRepositoryImpl) Upsert(ctx context.Context, monitorID string, infoJSON string) (*Model, error) {
	filter := bson.M{"monitor_id": monitorID}
	now := time.Now()

	update := bson.M{
		"$set": bson.M{
			"info_json":  infoJSON,
			"updated_at": now,
		},
		"$setOnInsert": bson.M{
			"_id":        primitive.NewObjectID(),
			"monitor_id": monitorID,
			"created_at": now,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := r.collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return nil, err
	}

	return r.GetByMonitorID(ctx, monitorID)
}

func (r *MongoRepositoryImpl) Delete(ctx context.Context, monitorID string) error {
	filter := bson.M{"monitor_id": monitorID}
	_, err := r.collection.DeleteOne(ctx, filter)
	return err
}

func (r *MongoRepositoryImpl) CleanupOldRecords(ctx context.Context, olderThanDays int) error {
	cutoffDate := time.Now().AddDate(0, 0, -olderThanDays)

	filter := bson.M{
		"updated_at": bson.M{"$lt": cutoffDate},
	}

	_, err := r.collection.DeleteMany(ctx, filter)
	return err
}
