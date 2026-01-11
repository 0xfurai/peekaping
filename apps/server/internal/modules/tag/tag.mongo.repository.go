package tag

import (
	"context"
	"errors"
	"time"
	"vigi/internal/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID          primitive.ObjectID `bson:"_id"`
	OrgID       string             `bson:"org_id"`
	Name        string             `bson:"name"`
	Color       string             `bson:"color"`
	Description *string            `bson:"description,omitempty"`
	CreatedAt   time.Time          `bson:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at"`
}

func toDomainModelFromMongo(mm *mongoModel) *Model {
	return &Model{
		ID:          mm.ID.Hex(),
		OrgID:       mm.OrgID,
		Name:        mm.Name,
		Color:       mm.Color,
		Description: mm.Description,
		CreatedAt:   mm.CreatedAt,
		UpdatedAt:   mm.UpdatedAt,
	}
}

func toMongoModel(m *Model) *mongoModel {
	var objID primitive.ObjectID
	if m.ID != "" {
		objID, _ = primitive.ObjectIDFromHex(m.ID)
	} else {
		objID = primitive.NewObjectID()
	}

	return &mongoModel{
		ID:          objID,
		OrgID:       m.OrgID,
		Name:        m.Name,
		Color:       m.Color,
		Description: m.Description,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

type MongoRepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("tags")
	ctx := context.Background()

	// Create indexes
	_, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		panic("Failed to create index on tag collection: " + err.Error())
	}

	return &MongoRepositoryImpl{client, db, collection}
}

func (r *MongoRepositoryImpl) Create(ctx context.Context, entity *Model) (*Model, error) {
	mm := toMongoModel(entity)
	mm.ID = primitive.NewObjectID()
	mm.CreatedAt = time.Now().UTC()
	mm.UpdatedAt = time.Now().UTC()

	_, err := r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromMongo(mm), nil
}

func (r *MongoRepositoryImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"_id": objectID, "org_id": orgID}
	var mm mongoModel
	err = r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromMongo(&mm), nil
}

func (r *MongoRepositoryImpl) FindByName(ctx context.Context, name string, orgID string) (*Model, error) {
	filter := bson.M{"name": name, "org_id": orgID}
	var mm mongoModel
	err := r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromMongo(&mm), nil
}

func (r *MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error) {
	var models []*Model

	skip := int64(page * limit)
	limit64 := int64(limit)

	options := &options.FindOptions{
		Skip:  &skip,
		Limit: &limit64,
		Sort:  bson.D{{Key: "name", Value: 1}},
	}

	filter := bson.M{"org_id": orgID}
	if q != "" {
		filter["name"] = bson.M{"$regex": q, "$options": "i"}
	}

	cursor, err := r.collection.Find(ctx, filter, options)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var mm mongoModel
		if err := cursor.Decode(&mm); err != nil {
			return nil, err
		}
		models = append(models, toDomainModelFromMongo(&mm))
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return models, nil
}

func (r *MongoRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *Model) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID, "org_id": entity.OrgID}
	mm := toMongoModel(entity)
	mm.UpdatedAt = time.Now().UTC()

	update := bson.M{
		"$set": bson.M{
			"name":        mm.Name,
			"color":       mm.Color,
			"description": mm.Description,
			"updated_at":  mm.UpdatedAt,
		},
	}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *MongoRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *UpdateModel) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID, "org_id": entity.OrgID}
	update := bson.M{"$set": bson.M{"updated_at": time.Now().UTC()}}

	if entity.Name != nil {
		update["$set"].(bson.M)["name"] = *entity.Name
	}
	if entity.Color != nil {
		update["$set"].(bson.M)["color"] = *entity.Color
	}
	if entity.Description != nil {
		update["$set"].(bson.M)["description"] = *entity.Description
	}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *MongoRepositoryImpl) Delete(ctx context.Context, id string, orgID string) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objID, "org_id": orgID}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}
