package notification_channel

import (
	"context"
	"errors"
	"time"
	"vigi/internal/config"
	"vigi/internal/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID        primitive.ObjectID `bson:"_id"`
	OrgID     string             `bson:"org_id"`
	Name      string             `bson:"name"`
	Type      string             `bson:"type"`
	Active    bool               `bson:"active"`
	IsDefault bool               `bson:"is_default"`
	Config    *string            `bson:"config,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

func toDomainModel(mm *mongoModel) *Model {
	return &Model{
		ID:        mm.ID.Hex(),
		OrgID:     mm.OrgID,
		Name:      mm.Name,
		Type:      mm.Type,
		Active:    mm.Active,
		IsDefault: mm.IsDefault,
		Config:    mm.Config,
		CreatedAt: mm.CreatedAt,
		UpdatedAt: mm.UpdatedAt,
	}
}

type RepositoryImpl struct {
	db         *mongo.Client
	collection *mongo.Collection
}

func NewMongoRepository(db *mongo.Client, cfg *config.Config) Repository {
	collection := db.Database(cfg.DBName).Collection("notification_channel")
	return &RepositoryImpl{db, collection}
}

func (r *RepositoryImpl) Create(ctx context.Context, entity *Model) (*Model, error) {
	now := time.Now()
	mm := &mongoModel{
		ID:        primitive.NewObjectID(),
		OrgID:     entity.OrgID,
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    entity.Config,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModel(mm), nil
}

func (r *RepositoryImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
	var mm mongoModel

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"_id": objectID}
	if orgID != "" {
		filter["org_id"] = orgID
	}
	err = r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(&mm), nil
}

func (r *RepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, orgID string) ([]*Model, error) {
	var entities []*mongoModel

	// Calculate the number of documents to skip
	skip := int64(page * limit)
	limit64 := int64(limit)

	// Build filter
	filter := bson.M{}
	if orgID != "" {
		filter["org_id"] = orgID
	}

	if q != "" {
		filter["$and"] = []bson.M{
			filter,
			{"$or": []bson.M{
				{"name": bson.M{"$regex": q, "$options": "i"}},
				{"type": bson.M{"$regex": q, "$options": "i"}},
			}},
		}
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
func (r *RepositoryImpl) UpdateFull(ctx context.Context, id string, entity *Model, orgID string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err // Return an error if the conversion fails
	}

	// Set UpdatedAt to current time
	entity.UpdatedAt = time.Now()

	filter := bson.M{"_id": objectID, "org_id": orgID}
	update := bson.M{"$set": entity}
	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *RepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *UpdateModel, orgID string) error {
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

	// Always set UpdatedAt to current time
	set["updated_at"] = time.Now()

	filter := bson.M{"_id": objectID, "org_id": orgID}
	update := bson.M{"$set": set}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *RepositoryImpl) Delete(ctx context.Context, id string, orgID string) error {
	objectId, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectId, "org_id": orgID}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}
