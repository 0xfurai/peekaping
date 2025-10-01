package user_workspace

import (
	"context"
	"errors"
	"peekaping/src/config"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type mongoModel struct {
	UserID      primitive.ObjectID `bson:"user_id"`
	WorkspaceID primitive.ObjectID `bson:"workspace_id"`
	Role        string             `bson:"role"`
	CreatedAt   time.Time          `bson:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at"`
}

type mongoUpdateModel struct {
	Role      *string    `bson:"role,omitempty"`
	UpdatedAt *time.Time `bson:"updated_at,omitempty"`
}

func toDomainModel(mm *mongoModel) *Model {
	return &Model{
		UserID:      mm.UserID.Hex(),
		WorkspaceID: mm.WorkspaceID.Hex(),
		Role:        mm.Role,
		CreatedAt:   mm.CreatedAt,
		UpdatedAt:   mm.UpdatedAt,
	}
}

type RepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("user_workspace")
	return &RepositoryImpl{client, db, collection}
}

func (r *RepositoryImpl) Create(ctx context.Context, userWorkspace *Model) (*Model, error) {
	userObjectID, err := primitive.ObjectIDFromHex(userWorkspace.UserID)
	if err != nil {
		return nil, err
	}

	workspaceObjectID, err := primitive.ObjectIDFromHex(userWorkspace.WorkspaceID)
	if err != nil {
		return nil, err
	}

	mm := &mongoModel{
		UserID:      userObjectID,
		WorkspaceID: workspaceObjectID,
		Role:        userWorkspace.Role,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	_, err = r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModel(mm), nil
}

func (r *RepositoryImpl) FindByUserID(ctx context.Context, userID string) ([]*Model, error) {
	userObjectID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"user_id": userObjectID}
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var userWorkspaces []*Model
	for cursor.Next(ctx) {
		var entity mongoModel
		if err := cursor.Decode(&entity); err != nil {
			return nil, err
		}
		userWorkspaces = append(userWorkspaces, toDomainModel(&entity))
	}

	return userWorkspaces, nil
}

func (r *RepositoryImpl) FindByWorkspaceID(ctx context.Context, workspaceID string) ([]*Model, error) {
	workspaceObjectID, err := primitive.ObjectIDFromHex(workspaceID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"workspace_id": workspaceObjectID}
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var userWorkspaces []*Model
	for cursor.Next(ctx) {
		var entity mongoModel
		if err := cursor.Decode(&entity); err != nil {
			return nil, err
		}
		userWorkspaces = append(userWorkspaces, toDomainModel(&entity))
	}

	return userWorkspaces, nil
}

func (r *RepositoryImpl) FindByUserAndWorkspace(ctx context.Context, userID, workspaceID string) (*Model, error) {
	userObjectID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}

	workspaceObjectID, err := primitive.ObjectIDFromHex(workspaceID)
	if err != nil {
		return nil, err
	}

	var entity mongoModel
	filter := bson.M{"user_id": userObjectID, "workspace_id": workspaceObjectID}
	err = r.collection.FindOne(ctx, filter).Decode(&entity)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(&entity), nil
}

func (r *RepositoryImpl) Update(ctx context.Context, userID, workspaceID string, userWorkspace *UpdateModel) error {
	userObjectID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return err
	}

	workspaceObjectID, err := primitive.ObjectIDFromHex(workspaceID)
	if err != nil {
		return err
	}

	updateModel := mongoUpdateModel{}
	if userWorkspace.Role != nil {
		updateModel.Role = userWorkspace.Role
	}
	now := time.Now()
	updateModel.UpdatedAt = &now

	filter := bson.M{"user_id": userObjectID, "workspace_id": workspaceObjectID}
	update := bson.M{"$set": updateModel}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *RepositoryImpl) Delete(ctx context.Context, userID, workspaceID string) error {
	userObjectID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return err
	}

	workspaceObjectID, err := primitive.ObjectIDFromHex(workspaceID)
	if err != nil {
		return err
	}

	filter := bson.M{"user_id": userObjectID, "workspace_id": workspaceObjectID}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}
