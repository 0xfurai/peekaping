package notification

import (
	"context"
	"errors"
	"peekaping/src/config"
	"peekaping/src/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID        primitive.ObjectID `bson:"_id"`
	Name      string             `bson:"name"`
	Type      string             `bson:"type"`
	Active    bool               `bson:"active"`
	IsDefault bool               `bson:"is_default"`
	Config    *string            `bson:"config,omitempty"`
}

func toDomainModel(mm *mongoModel) *Model {
	return &Model{
		ID:        mm.ID.Hex(),
		Name:      mm.Name,
		Type:      mm.Type,
		Active:    mm.Active,
		IsDefault: mm.IsDefault,
		Config:    mm.Config,
	}
}

type RepositoryImpl struct {
	db         *mongo.Client
	collection *mongo.Collection
}

func NewRepository(db *mongo.Client, cfg *config.Config) Repository {
	collection := db.Database(cfg.DBName).Collection("notification")
	return &RepositoryImpl{db, collection}
}

func (r *RepositoryImpl) Create(ctx context.Context, entity *Model) (*Model, error) {
	mm := &mongoModel{
		ID:        primitive.NewObjectID(),
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    entity.Config,
	}

	_, err := r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModel(mm), nil
}

func (r *RepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	var mm mongoModel

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"_id": objectID}
	err = r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(&mm), nil
}

func (r *RepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	var entities []*mongoModel

	// Calculate the number of documents to skip
	skip := int64((page) * limit)
	limit64 := int64(limit)

	// Build filter
	filter := bson.M{}
	if q != "" {
		filter = bson.M{"$or": []bson.M{
			{"name": bson.M{"$regex": q, "$options": "i"}},
			{"type": bson.M{"$regex": q, "$options": "i"}},
		}}
	}

	// Define options for pagination
	options := &options.FindOptions{
		Skip:  &skip,
		Limit: &limit64,
	}

	cursor, err := r.collection.Find(ctx, filter, options)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var entity mongoModel
		if err := cursor.Decode(&entity); err != nil {
			return nil, err
		}
		entities = append(entities, &entity)
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}

	domainEntities := make([]*Model, len(entities))
	for i, entity := range entities {
		domainEntities[i] = toDomainModel(entity)
	}

	return domainEntities, nil
}

// UpdateFull modifies an existing entity in the MongoDB collection.
func (r *RepositoryImpl) UpdateFull(ctx context.Context, id string, entity *Model) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err // Return an error if the conversion fails
	}

	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": entity}
	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *RepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *UpdateModel) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err // Return an error if the conversion fails
	}

	set, err := utils.ToBsonSet(entity)
	if err != nil {
		return err
	}

	if len(set) == 0 {
		return errors.New("Nothing to update")
	}

	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": set}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *RepositoryImpl) Delete(ctx context.Context, id string) error {
	objectId, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectId}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}
