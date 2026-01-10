package setting

import (
	"context"
	"vigi/internal/config"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID        primitive.ObjectID `bson:"_id"`
	Key       string             `bson:"key"`
	Value     string             `bson:"value"`
	Type      string             `bson:"type"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

// type mongoUpdateModel struct {
// 	Key       *string    `bson:"key,omitempty"`
// 	Value     *string    `bson:"value,omitempty"`
// 	UpdatedAt *time.Time `bson:"updated_at,omitempty"`
// }

func toDomainModel(mm *mongoModel) *Model {
	if mm == nil {
		return nil
	}
	return &Model{
		ID:        mm.ID.Hex(),
		Key:       mm.Key,
		Value:     mm.Value,
		Type:      mm.Type,
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
	collection := db.Collection("setting")
	return &MongoRepositoryImpl{client, db, collection}
}

func (r *MongoRepositoryImpl) GetByKey(ctx context.Context, key string) (*Model, error) {
	filter := bson.M{"key": key}
	var mm mongoModel
	err := r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(&mm), nil
}

func (r *MongoRepositoryImpl) SetByKey(ctx context.Context, key string, entity *CreateUpdateDto) (*Model, error) {
	now := time.Now().UTC()
	filter := bson.M{"key": key}
	update := bson.M{"$set": bson.M{
		"key":        key,
		"value":      entity.Value,
		"type":       entity.Type,
		"updated_at": now,
	}}
	options := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var mm mongoModel
	err := r.collection.FindOneAndUpdate(ctx, filter, update, options).Decode(&mm)
	if err != nil {
		return nil, err
	}
	return toDomainModel(&mm), nil
}

func (r *MongoRepositoryImpl) DeleteByKey(ctx context.Context, key string) error {
	filter := bson.M{"key": key}
	_, err := r.collection.DeleteOne(ctx, filter)
	return err
}
