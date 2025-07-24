package monitor_tag

import (
	"context"
	"errors"
	"peekaping/src/config"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID        primitive.ObjectID `bson:"_id"`
	MonitorID primitive.ObjectID `bson:"monitor_id"`
	TagID     primitive.ObjectID `bson:"tag_id"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

func toDomainModelFromMongo(mm *mongoModel) *Model {
	return &Model{
		ID:        mm.ID.Hex(),
		MonitorID: mm.MonitorID.Hex(),
		TagID:     mm.TagID.Hex(),
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
	collection := db.Collection("monitor_tags")

	// Create a unique index for monitor_id and tag_id
	_, err := collection.Indexes().CreateOne(context.TODO(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "monitor_id", Value: 1},
			{Key: "tag_id", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})

	if err != nil {
		panic("Failed to create index for monitor_tags: " + err.Error())
	}

	return &MongoRepositoryImpl{client, db, collection}
}

func (r *MongoRepositoryImpl) Create(ctx context.Context, model *Model) (*Model, error) {
	monitorObjectID, err := primitive.ObjectIDFromHex(model.MonitorID)
	if err != nil {
		return nil, err
	}

	tagObjectID, err := primitive.ObjectIDFromHex(model.TagID)
	if err != nil {
		return nil, err
	}

	mm := &mongoModel{
		ID:        primitive.NewObjectID(),
		MonitorID: monitorObjectID,
		TagID:     tagObjectID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	_, err = r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromMongo(mm), nil
}

func (r *MongoRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	var entity mongoModel
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"_id": objectID}
	err = r.collection.FindOne(ctx, filter).Decode(&entity)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromMongo(&entity), nil
}

func (r *MongoRepositoryImpl) FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
	monitorObjectID, err := primitive.ObjectIDFromHex(monitorID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"monitor_id": monitorObjectID}
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []*mongoModel
	for cursor.Next(ctx) {
		var entity mongoModel
		if err := cursor.Decode(&entity); err != nil {
			return nil, err
		}
		results = append(results, &entity)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}

	domainEntities := make([]*Model, len(results))
	for i, entity := range results {
		domainEntities[i] = toDomainModelFromMongo(entity)
	}

	return domainEntities, nil
}

func (r *MongoRepositoryImpl) FindByTagID(ctx context.Context, tagID string) ([]*Model, error) {
	tagObjectID, err := primitive.ObjectIDFromHex(tagID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"tag_id": tagObjectID}
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []*mongoModel
	for cursor.Next(ctx) {
		var entity mongoModel
		if err := cursor.Decode(&entity); err != nil {
			return nil, err
		}
		results = append(results, &entity)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}

	domainEntities := make([]*Model, len(results))
	for i, entity := range results {
		domainEntities[i] = toDomainModelFromMongo(entity)
	}

	return domainEntities, nil
}

func (r *MongoRepositoryImpl) Delete(ctx context.Context, id string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}

func (r *MongoRepositoryImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	monitorObjectID, err := primitive.ObjectIDFromHex(monitorID)
	if err != nil {
		return err
	}
	filter := bson.M{"monitor_id": monitorObjectID}
	_, err = r.collection.DeleteMany(ctx, filter)
	return err
}

func (r *MongoRepositoryImpl) DeleteByTagID(ctx context.Context, tagID string) error {
	tagObjectID, err := primitive.ObjectIDFromHex(tagID)
	if err != nil {
		return err
	}
	filter := bson.M{"tag_id": tagObjectID}
	_, err = r.collection.DeleteMany(ctx, filter)
	return err
}

func (r *MongoRepositoryImpl) DeleteByMonitorAndTag(ctx context.Context, monitorID string, tagID string) error {
	monitorObjectID, err := primitive.ObjectIDFromHex(monitorID)
	if err != nil {
		return err
	}
	tagObjectID, err := primitive.ObjectIDFromHex(tagID)
	if err != nil {
		return err
	}
	filter := bson.M{"monitor_id": monitorObjectID, "tag_id": tagObjectID}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}
