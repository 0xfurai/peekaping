package monitor_maintenance

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
	ID            primitive.ObjectID `bson:"_id"`
	MonitorID     primitive.ObjectID `bson:"monitor_id"`
	MaintenanceID primitive.ObjectID `bson:"maintenance_id"`
	CreatedAt     time.Time          `bson:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at"`
}

func toDomainModel(mm *mongoModel) *Model {
	return &Model{
		ID:            mm.ID.Hex(),
		MonitorID:     mm.MonitorID.Hex(),
		MaintenanceID: mm.MaintenanceID.Hex(),
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
	collection := db.Collection("monitor_maintenance")

	// Create a unique index for monitor_id and maintenance_id
	_, err := collection.Indexes().CreateOne(context.TODO(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "monitor_id", Value: 1},
			{Key: "maintenance_id", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})

	if err != nil {
		panic("Failed to create index for monitor_maintenance: " + err.Error())
	}

	return &RepositoryImpl{client, db, collection}
}

func (r *RepositoryImpl) Create(ctx context.Context, model *Model) (*Model, error) {
	monitorObjectID, err := primitive.ObjectIDFromHex(model.MonitorID)
	if err != nil {
		return nil, err
	}

	maintenanceObjectID, err := primitive.ObjectIDFromHex(model.MaintenanceID)
	if err != nil {
		return nil, err
	}

	mm := &mongoModel{
		ID:            primitive.NewObjectID(),
		MonitorID:     monitorObjectID,
		MaintenanceID: maintenanceObjectID,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	_, err = r.collection.InsertOne(ctx, mm)
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

func (r *RepositoryImpl) Delete(ctx context.Context, id string) error {
	objectId, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectId}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}

func (r *RepositoryImpl) FindByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
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
		domainEntities[i] = toDomainModel(entity)
	}

	return domainEntities, nil
}

func (r *RepositoryImpl) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	monitorObjectID, err := primitive.ObjectIDFromHex(monitorID)
	if err != nil {
		return err
	}
	filter := bson.M{"monitor_id": monitorObjectID}
	_, err = r.collection.DeleteMany(ctx, filter)
	return err
}

func (r *RepositoryImpl) DeleteByMaintenanceID(ctx context.Context, maintenanceID string) error {
	maintenanceObjectID, err := primitive.ObjectIDFromHex(maintenanceID)
	if err != nil {
		return err
	}
	filter := bson.M{"maintenance_id": maintenanceObjectID}
	_, err = r.collection.DeleteMany(ctx, filter)
	return err
}

func (r *RepositoryImpl) FindByMaintenanceID(ctx context.Context, maintenanceID string) ([]*Model, error) {
	maintenanceObjectID, err := primitive.ObjectIDFromHex(maintenanceID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"maintenance_id": maintenanceObjectID}
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
		domainEntities[i] = toDomainModel(entity)
	}

	return domainEntities, nil
}
