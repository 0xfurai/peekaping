package proxy

import (
	"context"
	"time"
	"vigi/internal/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID        primitive.ObjectID `bson:"_id"`
	OrgID     string             `bson:"org_id"`
	Protocol  string             `bson:"protocol"`
	Host      string             `bson:"host"`
	Port      int                `bson:"port"`
	Auth      bool               `bson:"auth"`
	Username  string             `bson:"username,omitempty"`
	Password  string             `bson:"password,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

type mongoUpdateModel struct {
	Protocol  *string    `bson:"protocol,omitempty"`
	Host      *string    `bson:"host,omitempty"`
	Port      *int       `bson:"port,omitempty"`
	Auth      *bool      `bson:"auth,omitempty"`
	Username  *string    `bson:"username,omitempty"`
	Password  *string    `bson:"password,omitempty"`
	UpdatedAt *time.Time `bson:"updated_at,omitempty"`
}

func toDomainModel(mm *mongoModel) *Model {
	return &Model{
		ID:        mm.ID.Hex(),
		OrgID:     mm.OrgID,
		Protocol:  mm.Protocol,
		Host:      mm.Host,
		Port:      mm.Port,
		Auth:      mm.Auth,
		Username:  mm.Username,
		Password:  mm.Password,
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
	collection := db.Collection("proxies")
	return &MongoRepositoryImpl{client, db, collection}
}

func (r *MongoRepositoryImpl) Create(ctx context.Context, entity *Model) (*Model, error) {
	mm := &mongoModel{
		ID:        primitive.NewObjectID(),
		OrgID:     entity.OrgID,
		Protocol:  entity.Protocol,
		Host:      entity.Host,
		Port:      entity.Port,
		Auth:      entity.Auth,
		Username:  entity.Username,
		Password:  entity.Password,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	_, err := r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModel(mm), nil
}

func (r *MongoRepositoryImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	filter := bson.M{
		"_id": objectID,
	}
	var mm mongoModel
	err = r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(&mm), nil
}

func (r *MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error) {
	var entities []*Model

	// Calculate the number of documents to skip
	skip := int64(page * limit)
	limit64 := int64(limit)

	// Define options for pagination
	options := &options.FindOptions{
		Skip:  &skip,
		Limit: &limit64,
		Sort:  bson.D{{Key: "created_at", Value: -1}},
	}

	filter := bson.M{}
	if q != "" {
		filter["$or"] = bson.A{
			bson.M{"protocol": bson.M{"$regex": q, "$options": "i"}},
			bson.M{"host": bson.M{"$regex": q, "$options": "i"}},
			bson.M{"username": bson.M{"$regex": q, "$options": "i"}},
		}
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
		entities = append(entities, toDomainModel(&mm))
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}

	return entities, nil
}

func (r *MongoRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *Model, orgID string) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	filter := bson.M{
		"_id":    objectID,
		"org_id": orgID,
	}

	now := time.Now().UTC()
	mm := mongoModel{
		ID:        objectID,
		Protocol:  entity.Protocol,
		Host:      entity.Host,
		Port:      entity.Port,
		Auth:      entity.Auth,
		Username:  entity.Username,
		Password:  entity.Password,
		UpdatedAt: now,
	}

	setFields, err := bson.Marshal(mm)
	if err != nil {
		return nil, err
	}

	var setMap bson.M
	if err := bson.Unmarshal(setFields, &setMap); err != nil {
		return nil, err
	}

	// Remove immutable fields from setMap
	delete(setMap, "_id")
	delete(setMap, "created_at")

	update := bson.M{"$set": setMap}

	result := r.collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After))
	if result.Err() != nil {
		return nil, result.Err()
	}

	var updatedMM mongoModel
	if err := result.Decode(&updatedMM); err != nil {
		return nil, err
	}

	return toDomainModel(&updatedMM), nil
}

func (r *MongoRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *UpdateModel, orgID string) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	filter := bson.M{
		"_id":    objectID,
		"org_id": orgID,
	}

	now := time.Now().UTC()
	updateModel := mongoUpdateModel{
		Protocol:  entity.Protocol,
		Host:      entity.Host,
		Port:      entity.Port,
		Auth:      entity.Auth,
		Username:  entity.Username,
		Password:  entity.Password,
		UpdatedAt: &now,
	}

	setFields, err := bson.Marshal(updateModel)
	if err != nil {
		return nil, err
	}

	var setMap bson.M
	if err := bson.Unmarshal(setFields, &setMap); err != nil {
		return nil, err
	}

	update := bson.M{"$set": setMap}

	result := r.collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After))
	if result.Err() != nil {
		return nil, result.Err()
	}

	var mm mongoModel
	if err := result.Decode(&mm); err != nil {
		return nil, err
	}

	return toDomainModel(&mm), nil
}

func (r *MongoRepositoryImpl) Delete(ctx context.Context, id string, orgID string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{
		"_id":    objectID,
		"org_id": orgID,
	}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}
