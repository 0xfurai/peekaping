package workspace

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
	Name      string             `bson:"name"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

type mongoUpdateModel struct {
	Name      *string    `bson:"name,omitempty"`
	UpdatedAt *time.Time `bson:"updated_at,omitempty"`
}

func toDomainModel(mm *mongoModel) *Model {
	return &Model{
		ID:        mm.ID.Hex(),
		Name:      mm.Name,
		CreatedAt: mm.CreatedAt,
		UpdatedAt: mm.UpdatedAt,
	}
}

type RepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("workspaces")
	return &RepositoryImpl{client, db, collection}
}

func (r *RepositoryImpl) Create(ctx context.Context, workspace *Model) (*Model, error) {
	mm := &mongoModel{
		ID:        primitive.NewObjectID(),
		Name:      workspace.Name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err := r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModel(mm), nil
}

func (r *RepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
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
	return toDomainModel(&entity), nil
}

func (r *RepositoryImpl) FindByIDs(ctx context.Context, workspaceIDs []string) ([]*Model, error) {
	if len(workspaceIDs) == 0 {
		return []*Model{}, nil
	}

	var objectIDs []primitive.ObjectID
	for _, id := range workspaceIDs {
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return nil, err
		}
		objectIDs = append(objectIDs, objectID)
	}

	filter := bson.M{"_id": bson.M{"$in": objectIDs}}
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var workspaces []*Model
	for cursor.Next(ctx) {
		var entity mongoModel
		if err := cursor.Decode(&entity); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, toDomainModel(&entity))
	}

	return workspaces, nil
}

func (r *RepositoryImpl) FindAll(ctx context.Context, page int, limit int) ([]*Model, error) {
	offset := int64((page - 1) * limit)
	findOptions := options.Find()
	findOptions.SetSkip(offset)
	findOptions.SetLimit(int64(limit))

	cursor, err := r.collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var workspaces []*Model
	for cursor.Next(ctx) {
		var entity mongoModel
		if err := cursor.Decode(&entity); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, toDomainModel(&entity))
	}

	return workspaces, nil
}

func (r *RepositoryImpl) Update(ctx context.Context, id string, workspace *UpdateModel) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	updateModel := mongoUpdateModel{}
	if workspace.Name != nil {
		updateModel.Name = workspace.Name
	}
	now := time.Now()
	updateModel.UpdatedAt = &now

	filter := bson.M{"_id": objectID}
	update := bson.M{"$set": updateModel}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *RepositoryImpl) Delete(ctx context.Context, id string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}
