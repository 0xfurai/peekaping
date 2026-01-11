package maintenance

import (
	"context"
	"fmt"
	"time"
	"vigi/internal/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID            primitive.ObjectID `bson:"_id"`
	OrgID         string             `bson:"org_id"`
	Title         string             `bson:"title"`
	Description   string             `bson:"description"`
	Active        bool               `bson:"active"`
	Strategy      string             `bson:"strategy"`
	StartDateTime *string            `bson:"start_date_time,omitempty"`
	EndDateTime   *string            `bson:"end_date_time,omitempty"`
	StartTime     *string            `bson:"start_time,omitempty"`
	EndTime       *string            `bson:"end_time,omitempty"`
	Weekdays      []int              `bson:"weekdays,omitempty"`
	DaysOfMonth   []int              `bson:"days_of_month,omitempty"`
	IntervalDay   *int               `bson:"interval_day,omitempty"`
	Cron          *string            `bson:"cron,omitempty"`
	Timezone      *string            `bson:"timezone,omitempty"`
	Duration      *int               `bson:"duration,omitempty"`
	CreatedAt     time.Time          `bson:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at"`
}

type mongoUpdateModel struct {
	Title         *string `bson:"title,omitempty"`
	Description   *string `bson:"description,omitempty"`
	Active        *bool   `bson:"active,omitempty"`
	Strategy      *string `bson:"strategy,omitempty"`
	StartDateTime *string `bson:"start_date_time,omitempty"`
	EndDateTime   *string `bson:"end_date_time,omitempty"`
	StartTime     *string `bson:"start_time,omitempty"`
	EndTime       *string `bson:"end_time,omitempty"`
	Weekdays      []int   `bson:"weekdays,omitempty"`
	DaysOfMonth   []int   `bson:"days_of_month,omitempty"`
	IntervalDay   *int    `bson:"interval_day,omitempty"`
	Cron          *string `bson:"cron,omitempty"`
	Timezone      *string `bson:"timezone,omitempty"`
	Duration      *int    `bson:"duration,omitempty"`
	UpdatedAt     *string `bson:"updated_at,omitempty"`
}

func toDomainModel(mm *mongoModel) *Model {
	return &Model{
		ID:            mm.ID.Hex(),
		OrgID:         mm.OrgID,
		Title:         mm.Title,
		Description:   mm.Description,
		Active:        mm.Active,
		Strategy:      mm.Strategy,
		StartDateTime: mm.StartDateTime,
		EndDateTime:   mm.EndDateTime,
		StartTime:     mm.StartTime,
		EndTime:       mm.EndTime,
		Weekdays:      mm.Weekdays,
		DaysOfMonth:   mm.DaysOfMonth,
		IntervalDay:   mm.IntervalDay,
		Cron:          mm.Cron,
		Timezone:      mm.Timezone,
		Duration:      mm.Duration,
		CreatedAt:     mm.CreatedAt,
		UpdatedAt:     mm.UpdatedAt,
	}
}

type MongoRepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("maintenance")

	_, err := collection.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "monitor_id", Value: 1}},
	})
	if err != nil {
		panic("Failed to create index on maintenance collection: " + err.Error())
	}

	return &MongoRepositoryImpl{client, db, collection}
}

func (r *MongoRepositoryImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	mm := &mongoModel{
		ID:            primitive.NewObjectID(),
		Title:         entity.Title,
		Description:   entity.Description,
		Active:        entity.Active,
		Strategy:      entity.Strategy,
		StartDateTime: entity.StartDateTime,
		EndDateTime:   entity.EndDateTime,
		Weekdays:      entity.Weekdays,
		DaysOfMonth:   entity.DaysOfMonth,
		IntervalDay:   entity.IntervalDay,
		Cron:          entity.Cron,
		Timezone:      entity.Timezone,
		Duration:      entity.Duration,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
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

	filter := bson.M{"_id": objectID}
	if orgID != "" {
		filter["org_id"] = orgID
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

func (r *MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string, strategy string, orgID string) ([]*Model, error) {
	var entities []*Model

	// Calculate the number of documents to skip
	skip := int64(page * limit)
	limit64 := int64(limit)

	// Define options for pagination and sorting
	options := &options.FindOptions{
		Skip:  &skip,
		Limit: &limit64,
		Sort:  bson.D{{Key: "updated_at", Value: -1}}, // Sort by updated_at in descending order
	}

	filter := bson.M{}
	if orgID != "" {
		filter["org_id"] = orgID
	}
	if q != "" {
		filter["$or"] = bson.A{
			bson.M{"title": bson.M{"$regex": q, "$options": "i"}},
			bson.M{"description": bson.M{"$regex": q, "$options": "i"}},
		}
	}

	if strategy != "" {
		filter["strategy"] = strategy
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

func (r *MongoRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto, orgID string) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	mm := &mongoModel{
		ID:            objectID,
		Title:         entity.Title,
		Description:   entity.Description,
		Active:        entity.Active,
		Strategy:      entity.Strategy,
		StartDateTime: entity.StartDateTime,
		EndDateTime:   entity.EndDateTime,
		StartTime:     entity.StartTime,
		EndTime:       entity.EndTime,
		Weekdays:      entity.Weekdays,
		DaysOfMonth:   entity.DaysOfMonth,
		IntervalDay:   entity.IntervalDay,
		Cron:          entity.Cron,
		Timezone:      entity.Timezone,
		Duration:      entity.Duration,
		UpdatedAt:     time.Now(),
	}

	filter := bson.M{"_id": objectID, "org_id": orgID}
	update := bson.M{"$set": mm}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return nil, err
	}

	return toDomainModel(mm), nil
}

func (r *MongoRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto, orgID string) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	update := &mongoUpdateModel{
		Title:         entity.Title,
		Description:   entity.Description,
		Active:        entity.Active,
		Strategy:      entity.Strategy,
		StartDateTime: entity.StartDateTime,
		EndDateTime:   entity.EndDateTime,
		StartTime:     entity.StartTime,
		EndTime:       entity.EndTime,
		Weekdays:      entity.Weekdays,
		DaysOfMonth:   entity.DaysOfMonth,
		IntervalDay:   entity.IntervalDay,
		Cron:          entity.Cron,
		Timezone:      entity.Timezone,
		Duration:      entity.Duration,
		UpdatedAt:     &nowStr,
	}

	filter := bson.M{"_id": objectID, "org_id": orgID}
	updateDoc := bson.M{"$set": update}

	_, err = r.collection.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		return nil, err
	}

	// Get the updated document
	var mm mongoModel
	err = r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		return nil, err
	}

	return toDomainModel(&mm), nil
}

func (r *MongoRepositoryImpl) Delete(ctx context.Context, id string, orgID string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID, "org_id": orgID}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}

func (r *MongoRepositoryImpl) SetActive(ctx context.Context, id string, active bool, orgID string) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	fmt.Println("Setting active to", active)
	now := time.Now().UTC().Format(time.RFC3339)
	update := bson.M{"$set": bson.M{"active": active, "updated_at": now}}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": objectID, "org_id": orgID}, update)
	if err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id, orgID)
}

// GetMaintenancesByMonitorID returns all active maintenances for a given monitor_id
func (r *MongoRepositoryImpl) GetMaintenancesByMonitorID(ctx context.Context, monitorID string) ([]*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(monitorID)
	if err != nil {
		return nil, err
	}
	coll := r.db.Collection("monitor_maintenance")
	// Find all maintenance_ids for this monitor
	cursor, err := coll.Find(ctx, bson.M{"monitor_id": objectID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var maintenanceIDs []primitive.ObjectID
	for cursor.Next(ctx) {
		var doc struct {
			MaintenanceID string `bson:"maintenance_id"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		objectID, err := primitive.ObjectIDFromHex(doc.MaintenanceID)
		if err == nil {
			maintenanceIDs = append(maintenanceIDs, objectID)
		}
	}
	if len(maintenanceIDs) == 0 {
		return nil, nil
	}
	// Now fetch all active maintenances with these IDs
	filter := bson.M{"_id": bson.M{"$in": maintenanceIDs}, "active": true}
	cursor2, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor2.Close(ctx)
	var maintenances []*Model
	for cursor2.Next(ctx) {
		var mm mongoModel
		if err := cursor2.Decode(&mm); err != nil {
			return nil, err
		}
		maintenances = append(maintenances, toDomainModel(&mm))
	}
	return maintenances, nil
}
