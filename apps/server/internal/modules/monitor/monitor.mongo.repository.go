package monitor

import (
	"context"
	"errors"
	"fmt"
	"time"
	"vigi/internal/config"
	"vigi/internal/modules/heartbeat"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID             primitive.ObjectID      `bson:"_id"`
	Type           string                  `bson:"type"`
	Name           string                  `bson:"name"`
	Interval       int                     `bson:"interval"`
	Timeout        int                     `bson:"timeout"`
	MaxRetries     int                     `bson:"max_retries"`
	RetryInterval  int                     `bson:"retry_interval"`
	ResendInterval int                     `bson:"resend_interval"`
	Active         bool                    `bson:"active"`
	Status         heartbeat.MonitorStatus `bson:"status"`
	CreatedAt      time.Time               `bson:"created_at"`
	UpdatedAt      time.Time               `bson:"updated_at"`
	Config         string                  `bson:"config"`
	ProxyId        *primitive.ObjectID     `bson:"proxy_id,omitempty"`
	PushToken      string                  `bson:"push_token"`
}

type mongoUpdateModel struct {
	Type           *string                  `bson:"type,omitempty"`
	Name           *string                  `bson:"name,omitempty"`
	Interval       *int                     `bson:"interval,omitempty"`
	Timeout        *int                     `bson:"timeout,omitempty"`
	MaxRetries     *int                     `bson:"max_retries,omitempty"`
	RetryInterval  *int                     `bson:"retry_interval,omitempty"`
	ResendInterval *int                     `bson:"resend_interval,omitempty"`
	Active         *bool                    `bson:"active,omitempty"`
	Status         *heartbeat.MonitorStatus `bson:"status,omitempty"`
	Config         *string                  `bson:"config,omitempty"`
	ProxyId        *primitive.ObjectID      `bson:"proxy_id,omitempty"`
	PushToken      *string                  `bson:"push_token,omitempty"`
	CreatedAt      *time.Time               `bson:"created_at,omitempty"`
	UpdatedAt      *time.Time               `bson:"updated_at,omitempty"`
}

func toDomainModel(mm *mongoModel) *Model {
	var proxyId string
	if mm.ProxyId != nil {
		proxyId = mm.ProxyId.Hex()
	} else {
		proxyId = ""
	}
	return &Model{
		ID:             mm.ID.Hex(),
		Type:           mm.Type,
		Name:           mm.Name,
		Interval:       mm.Interval,
		Timeout:        mm.Timeout,
		MaxRetries:     mm.MaxRetries,
		RetryInterval:  mm.RetryInterval,
		ResendInterval: mm.ResendInterval,
		Active:         mm.Active,
		Status:         mm.Status,
		Config:         mm.Config,
		ProxyId:        proxyId,
		PushToken:      mm.PushToken,
		CreatedAt:      mm.CreatedAt,
		UpdatedAt:      mm.UpdatedAt,
	}
}

type MonitorRepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) MonitorRepository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("monitor")
	ctx := context.Background()

	_, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "active", Value: 1},
			{Key: "status", Value: 1},
			{Key: "created_at", Value: -1},
		},
	})
	if err != nil {
		panic("Failed to create index on monitor collection:" + err.Error())
	}

	_, err = collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "name", Value: 1}},
	})
	if err != nil {
		panic("Failed to create index on monitor collection:" + err.Error())
	}

	_, err = collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "url", Value: 1}},
	})
	if err != nil {
		panic("Failed to create index on monitor collection:" + err.Error())
	}

	_, err = collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "push_token", Value: 1}},
	})
	if err != nil {
		panic("Failed to create index on monitor collection:" + err.Error())
	}

	return &MonitorRepositoryImpl{client, db, collection}
}

func (r *MonitorRepositoryImpl) Create(ctx context.Context, monitor *Model) (*Model, error) {
	var proxyObjectID *primitive.ObjectID
	if monitor.ProxyId != "" {
		objID, err := primitive.ObjectIDFromHex(monitor.ProxyId)
		if err != nil {
			return nil, err
		}
		proxyObjectID = &objID
	}

	mm := &mongoModel{
		ID:             primitive.NewObjectID(),
		Type:           monitor.Type,
		Name:           monitor.Name,
		Interval:       monitor.Interval,
		Timeout:        monitor.Timeout,
		MaxRetries:     monitor.MaxRetries,
		RetryInterval:  monitor.RetryInterval,
		ResendInterval: monitor.ResendInterval,
		Active:         monitor.Active,
		Status:         0,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		Config:         monitor.Config,
		ProxyId:        proxyObjectID,
		PushToken:      monitor.PushToken,
	}

	_, err := r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModel(mm), nil
}

func (r *MonitorRepositoryImpl) FindByID(ctx context.Context, id string, orgID string) (*Model, error) {
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
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(&mm), nil
}

func (r *MonitorRepositoryImpl) FindAll(
	ctx context.Context,
	page int,
	limit int,
	q string,
	active *bool,
	status *int,
	tagIds []string,
	orgID string,
) ([]*Model, error) {
	var monitors []*Model

	// Calculate the number of documents to skip
	skip := int64(page * limit)
	limit64 := int64(limit)

	// If tagIds filtering is requested, use aggregation pipeline
	if len(tagIds) > 0 {
		// Convert tagIds to ObjectIDs
		var tagObjectIDs []primitive.ObjectID
		for _, tagId := range tagIds {
			objectID, err := primitive.ObjectIDFromHex(tagId)
			if err != nil {
				return nil, err
			}
			tagObjectIDs = append(tagObjectIDs, objectID)
		}

		// Build aggregation pipeline
		pipeline := bson.A{
			// Lookup monitor_tags to get monitors with specified tags
			bson.M{
				"$lookup": bson.M{
					"from":         "monitor_tags",
					"localField":   "_id",
					"foreignField": "monitor_id",
					"as":           "tags",
				},
			},
			// Match monitors that have at least one of the specified tags
			bson.M{
				"$match": bson.M{
					"tags.tag_id": bson.M{"$in": tagObjectIDs},
				},
			},
		}

		// Add additional filters
		matchStage := bson.M{}
		if q != "" {
			matchStage["$or"] = bson.A{
				bson.M{"name": bson.M{"$regex": q, "$options": "i"}},
				bson.M{"url": bson.M{"$regex": q, "$options": "i"}},
			}
		}
		if active != nil {
			matchStage["active"] = *active
		}
		if status != nil {
			matchStage["status"] = *status
		}

		// Add the additional match stage if there are filters
		if len(matchStage) > 0 {
			pipeline = append(pipeline, bson.M{"$match": matchStage})
		}

		// Add sorting, skip, and limit
		pipeline = append(pipeline,
			bson.M{"$sort": bson.M{"created_at": -1}},
			bson.M{"$skip": skip},
			bson.M{"$limit": limit64},
		)

		cursor, err := r.collection.Aggregate(ctx, pipeline)
		if err != nil {
			return nil, err
		}
		defer cursor.Close(ctx)

		for cursor.Next(ctx) {
			var mm mongoModel
			if err := cursor.Decode(&mm); err != nil {
				return nil, err
			}
			monitors = append(monitors, toDomainModel(&mm))
		}

		if err := cursor.Err(); err != nil {
			return nil, err
		}
	} else {
		// Use regular find when no tag filtering is needed
		// Define options for pagination
		options := &options.FindOptions{
			Skip:  &skip,
			Limit: &limit64,
			Sort:  bson.D{{Key: "created_at", Value: -1}},
		}

		filter := bson.M{}
		if q != "" {
			filter["$or"] = bson.A{
				bson.M{"name": bson.M{"$regex": q, "$options": "i"}},
				bson.M{"url": bson.M{"$regex": q, "$options": "i"}},
			}
		}
		if active != nil {
			filter["active"] = *active
		}
		if status != nil {
			filter["status"] = *status
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
			monitors = append(monitors, toDomainModel(&mm))
		}

		if err := cursor.Err(); err != nil {
			return nil, err
		}
	}

	return monitors, nil
}

func buildSetMapFromModelForUpdate(m *Model, preserveCreatedAt time.Time, includeProxyId bool, proxyObjectID primitive.ObjectID) bson.M {
	set := bson.M{
		"type":            m.Type,
		"name":            m.Name,
		"interval":        m.Interval,
		"timeout":         m.Timeout,
		"max_retries":     m.MaxRetries,
		"retry_interval":  m.RetryInterval,
		"resend_interval": m.ResendInterval,
		"active":          m.Active,
		"status":          0,                 // or m.Status if available
		"created_at":      preserveCreatedAt, // Preserve original created_at
		"updated_at":      time.Now().UTC(),
		"config":          m.Config,
	}
	if includeProxyId {
		set["proxy_id"] = proxyObjectID
	}
	return set
}

func buildSetMapFromUpdateModel(mu *mongoUpdateModel, includeProxyId bool, proxyObjectID *primitive.ObjectID) (bson.M, error) {
	set := bson.M{}
	if mu.Type != nil {
		set["type"] = *mu.Type
	}
	if mu.Name != nil {
		set["name"] = *mu.Name
	}
	if mu.Interval != nil {
		set["interval"] = *mu.Interval
	}
	if mu.Timeout != nil {
		set["timeout"] = *mu.Timeout
	}
	if mu.MaxRetries != nil {
		set["max_retries"] = *mu.MaxRetries
	}
	if mu.RetryInterval != nil {
		set["retry_interval"] = *mu.RetryInterval
	}
	if mu.ResendInterval != nil {
		set["resend_interval"] = *mu.ResendInterval
	}
	if mu.Active != nil {
		set["active"] = *mu.Active
	}
	if mu.Status != nil {
		set["status"] = *mu.Status
	}
	if mu.CreatedAt != nil {
		set["created_at"] = *mu.CreatedAt
	}
	if mu.UpdatedAt != nil {
		set["updated_at"] = *mu.UpdatedAt
	}
	if mu.Config != nil {
		set["config"] = *mu.Config
	}
	if includeProxyId && proxyObjectID != nil {
		set["proxy_id"] = *proxyObjectID
	}
	return set, nil
}

func (r *MonitorRepositoryImpl) UpdateFull(ctx context.Context, id string, monitor *Model, orgID string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	// Fetch existing monitor to preserve created_at
	existingMonitor, err := r.FindByID(ctx, id, orgID)
	if err != nil {
		return err
	}
	if existingMonitor == nil {
		return fmt.Errorf("%w: monitor %s", ErrMonitorNotFound, id)
	}

	filter := bson.M{"_id": objectID}
	if orgID != "" {
		filter["org_id"] = orgID
	}
	update := bson.M{}

	if monitor.ProxyId == "" {
		set := buildSetMapFromModelForUpdate(monitor, existingMonitor.CreatedAt, false, primitive.NilObjectID)
		update["$set"] = set
		update["$unset"] = bson.M{"proxy_id": ""}
	} else {
		proxyObjectID, err := primitive.ObjectIDFromHex(monitor.ProxyId)
		if err != nil {
			return err
		}
		set := buildSetMapFromModelForUpdate(monitor, existingMonitor.CreatedAt, true, proxyObjectID)
		update["$set"] = set
	}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *MonitorRepositoryImpl) UpdatePartial(ctx context.Context, id string, monitor *UpdateModel, orgID string) error {
	var proxyObjectID *primitive.ObjectID
	unsetProxyId := false

	if monitor.ProxyId != nil {
		if *monitor.ProxyId == "" {
			unsetProxyId = true
		} else {
			objectID, err := primitive.ObjectIDFromHex(*monitor.ProxyId)
			if err != nil {
				return err
			}
			proxyObjectID = &objectID
		}
	}

	mu := &mongoUpdateModel{
		Type:           monitor.Type,
		Name:           monitor.Name,
		Interval:       monitor.Interval,
		Timeout:        monitor.Timeout,
		MaxRetries:     monitor.MaxRetries,
		RetryInterval:  monitor.RetryInterval,
		ResendInterval: monitor.ResendInterval,
		Active:         monitor.Active,
		Status:         monitor.Status,
		CreatedAt:      monitor.CreatedAt,
		UpdatedAt:      monitor.UpdatedAt,
		Config:         monitor.Config,
		ProxyId:        proxyObjectID,
		PushToken:      monitor.PushToken,
	}

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	set, err := buildSetMapFromUpdateModel(mu, !unsetProxyId && proxyObjectID != nil, proxyObjectID)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID}
	if orgID != "" {
		filter["org_id"] = orgID
	}
	update := bson.M{}
	if len(set) > 0 {
		update["$set"] = set
	}
	if unsetProxyId {
		update["$unset"] = bson.M{"proxy_id": ""}
	}

	if len(update) == 0 {
		return errors.New("nothing to update")
	}

	_, err = r.collection.UpdateOne(ctx, filter, update)
	return err
}

// Delete removes a monitor from the MongoDB collection by its ID.
func (r *MonitorRepositoryImpl) Delete(ctx context.Context, id string, orgID string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID}
	if orgID != "" {
		filter["org_id"] = orgID
	}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}

// FindActive retrieves all active monitors from the MongoDB collection.
func (r *MonitorRepositoryImpl) FindActive(ctx context.Context) ([]*Model, error) {
	var monitors []*Model

	// Define options for pagination
	options := &options.FindOptions{
		Sort: bson.D{{Key: "_id", Value: -1}},
	}

	// Filter for active monitors
	filter := bson.M{"active": true}

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
		monitors = append(monitors, toDomainModel(&mm))
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return monitors, nil
}

// FindActivePaginated retrieves active monitors from the MongoDB collection with pagination.
func (r *MonitorRepositoryImpl) FindActivePaginated(ctx context.Context, page int, limit int) ([]*Model, error) {
	var monitors []*Model

	// Calculate skip value
	skip := int64(page * limit)
	limit64 := int64(limit)

	// Define options for pagination
	options := &options.FindOptions{
		Skip:  &skip,
		Limit: &limit64,
		Sort:  bson.D{{Key: "_id", Value: -1}},
	}

	// Filter for active monitors
	filter := bson.M{"active": true}

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
		monitors = append(monitors, toDomainModel(&mm))
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return monitors, nil
}

// RemoveProxyReference sets proxy_id to an empty string for all monitors with the given proxyId.
func (r *MonitorRepositoryImpl) RemoveProxyReference(ctx context.Context, proxyId string) error {
	objectID, err := primitive.ObjectIDFromHex(proxyId)
	if err != nil {
		return err
	}

	filter := bson.M{"proxy_id": objectID}
	update := bson.M{"$set": bson.M{"proxy_id": ""}}
	_, err = r.collection.UpdateMany(ctx, filter, update)
	return err
}

// FindByProxyId returns all monitors using the given proxyId
func (r *MonitorRepositoryImpl) FindByProxyId(ctx context.Context, proxyId string) ([]*Model, error) {
	var monitors []*Model

	objectID, err := primitive.ObjectIDFromHex(proxyId)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"proxy_id": objectID}
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var mm mongoModel
		if err := cursor.Decode(&mm); err != nil {
			return nil, err
		}
		monitors = append(monitors, toDomainModel(&mm))
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return monitors, nil
}

func (r *MonitorRepositoryImpl) FindOneByPushToken(ctx context.Context, pushToken string) (*Model, error) {
	filter := bson.M{
		"type":       "push",
		"push_token": pushToken,
	}
	var mm mongoModel
	err := r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModel(&mm), nil
}

func (r *MonitorRepositoryImpl) FindByIDs(ctx context.Context, ids []string, orgID string) ([]*Model, error) {
	var monitors []*Model
	var objectIDs []primitive.ObjectID

	// Convert string IDs to ObjectIDs
	for _, id := range ids {
		objectID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return nil, err
		}
		objectIDs = append(objectIDs, objectID)
	}

	// Create filter for the IDs
	filter := bson.M{"_id": bson.M{"$in": objectIDs}}
	if orgID != "" {
		filter["org_id"] = orgID
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var mm mongoModel
		if err := cursor.Decode(&mm); err != nil {
			return nil, err
		}
		monitors = append(monitors, toDomainModel(&mm))
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}

	return monitors, nil
}
