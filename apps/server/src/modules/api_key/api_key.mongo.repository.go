package api_key

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
	ID             primitive.ObjectID `bson:"_id"`
	UserID         string             `bson:"user_id"`
	Name           string             `bson:"name"`
	KeyHash        string             `bson:"key_hash"`
	DisplayKey     string             `bson:"display_key"`
	LastUsed       *time.Time         `bson:"last_used"`
	ExpiresAt      *time.Time         `bson:"expires_at"`
	UsageCount     int64              `bson:"usage_count"`
	MaxUsageCount  *int64             `bson:"max_usage_count"`
	CreatedAt      time.Time          `bson:"createdAt"`
	UpdatedAt      time.Time          `bson:"updatedAt"`
}

type mongoUpdateModel struct {
	Name          *string    `bson:"name,omitempty"`
	ExpiresAt     *time.Time `bson:"expires_at,omitempty"`
	MaxUsageCount *int64     `bson:"max_usage_count,omitempty"`
	UpdatedAt     *time.Time `bson:"updatedAt,omitempty"`
}

func toDomainModel(mm *mongoModel) *Model {
	return &Model{
		ID:            mm.ID.Hex(),
		UserID:        mm.UserID,
		Name:          mm.Name,
		KeyHash:       mm.KeyHash,
		DisplayKey:    mm.DisplayKey,
		LastUsed:      mm.LastUsed,
		ExpiresAt:     mm.ExpiresAt,
		UsageCount:    mm.UsageCount,
		MaxUsageCount: mm.MaxUsageCount,
		CreatedAt:     mm.CreatedAt,
		UpdatedAt:     mm.UpdatedAt,
	}
}

type RepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("api_keys")
	return &RepositoryImpl{client, db, collection}
}

func (r *RepositoryImpl) Create(ctx context.Context, apiKey *CreateModel) (*APIKeyWithToken, error) {
	// Generate a secure API key
	token, keyHash, displayKey, err := generateAPIKey()
	if err != nil {
		return nil, err
	}

	mm := &mongoModel{
		ID:            primitive.NewObjectID(),
		UserID:        apiKey.UserID,
		Name:          apiKey.Name,
		KeyHash:       keyHash,
		DisplayKey:    displayKey,
		LastUsed:      nil,
		ExpiresAt:     apiKey.ExpiresAt,
		UsageCount:    0,
		MaxUsageCount: apiKey.MaxUsageCount,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	_, err = r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	domainModel := toDomainModel(mm)
	return &APIKeyWithToken{
		Model: *domainModel,
		Token: token,
	}, nil
}

func (r *RepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	mm := new(mongoModel)
	err = r.collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(mm)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(mm), nil
}

func (r *RepositoryImpl) FindByUserID(ctx context.Context, userID string) ([]*Model, error) {
	opts := options.Find().SetSort(bson.M{"createdAt": -1})
	cursor, err := r.collection.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var mms []*mongoModel
	if err = cursor.All(ctx, &mms); err != nil {
		return nil, err
	}

	models := make([]*Model, len(mms))
	for i, mm := range mms {
		models[i] = toDomainModel(mm)
	}
	return models, nil
}

func (r *RepositoryImpl) FindByKeyHash(ctx context.Context, keyHash string) (*Model, error) {
	mm := new(mongoModel)
	err := r.collection.FindOne(ctx, bson.M{"key_hash": keyHash}).Decode(mm)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(mm), nil
}

func (r *RepositoryImpl) Update(ctx context.Context, id string, update *UpdateModel) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	updateDoc := mongoUpdateModel{
		UpdatedAt: &[]time.Time{time.Now()}[0],
	}

	if update.Name != nil {
		updateDoc.Name = update.Name
	}
	if update.ExpiresAt != nil {
		updateDoc.ExpiresAt = update.ExpiresAt
	}
	if update.MaxUsageCount != nil {
		updateDoc.MaxUsageCount = update.MaxUsageCount
	}

	mm := new(mongoModel)
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err = r.collection.FindOneAndUpdate(
		ctx,
		bson.M{"_id": objectID},
		bson.M{"$set": updateDoc},
		opts,
	).Decode(mm)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}

	return toDomainModel(mm), nil
}

func (r *RepositoryImpl) Delete(ctx context.Context, id string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	_, err = r.collection.DeleteOne(ctx, bson.M{"_id": objectID})
	return err
}

func (r *RepositoryImpl) FindAll(ctx context.Context) ([]*Model, error) {
	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var mongoModels []mongoModel
	if err = cursor.All(ctx, &mongoModels); err != nil {
		return nil, err
	}

	models := make([]*Model, len(mongoModels))
	for i, mongoModel := range mongoModels {
		models[i] = toDomainModel(&mongoModel)
	}

	return models, nil
}

func (r *RepositoryImpl) UpdateLastUsed(ctx context.Context, id string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	_, err = r.collection.UpdateOne(
		ctx,
		bson.M{"_id": objectID},
		bson.M{
			"$set": bson.M{"last_used": time.Now()},
			"$inc": bson.M{"usage_count": 1},
		},
	)
	return err
}
