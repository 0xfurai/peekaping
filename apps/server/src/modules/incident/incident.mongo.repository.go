package incident

import (
	"context"
	"peekaping/src/config"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID           primitive.ObjectID  `bson:"_id"`
	Title        string              `bson:"title"`
	Content      string              `bson:"content"`
	Style        string              `bson:"style"`
	Pin          bool                `bson:"pin"`
	Active       bool                `bson:"active"`
	StatusPageID *primitive.ObjectID `bson:"status_page_id,omitempty"`
	CreatedAt    time.Time           `bson:"created_at"`
	UpdatedAt    time.Time           `bson:"updated_at"`
}

type mongoUpdateModel struct {
	Title        *string             `bson:"title,omitempty"`
	Content      *string             `bson:"content,omitempty"`
	Style        *string             `bson:"style,omitempty"`
	Pin          *bool               `bson:"pin,omitempty"`
	Active       *bool               `bson:"active,omitempty"`
	StatusPageID *primitive.ObjectID `bson:"status_page_id,omitempty"`
	UpdatedAt    time.Time           `bson:"updated_at"`
}

func toDomainModel(mm *mongoModel) *Model {
	var statusPageID *string
	if mm.StatusPageID != nil {
		id := mm.StatusPageID.Hex()
		statusPageID = &id
	}

	return &Model{
		ID:           mm.ID.Hex(),
		Title:        mm.Title,
		Content:      mm.Content,
		Style:        mm.Style,
		Pin:          mm.Pin,
		Active:       mm.Active,
		StatusPageID: statusPageID,
		CreatedAt:    mm.CreatedAt,
		UpdatedAt:    mm.UpdatedAt,
	}
}

func toMongoModel(m *Model) (*mongoModel, error) {
	objID, err := primitive.ObjectIDFromHex(m.ID)
	if err != nil {
		objID = primitive.NewObjectID()
	}

	var statusPageObjID *primitive.ObjectID
	if m.StatusPageID != nil && *m.StatusPageID != "" {
		id, err := primitive.ObjectIDFromHex(*m.StatusPageID)
		if err != nil {
			return nil, err
		}
		statusPageObjID = &id
	}

	return &mongoModel{
		ID:           objID,
		Title:        m.Title,
		Content:      m.Content,
		Style:        m.Style,
		Pin:          m.Pin,
		Active:       m.Active,
		StatusPageID: statusPageObjID,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}, nil
}

func toMongoUpdateModel(m *UpdateModel) (*mongoUpdateModel, error) {
	update := &mongoUpdateModel{
		Title:     m.Title,
		Content:   m.Content,
		Style:     m.Style,
		Pin:       m.Pin,
		Active:    m.Active,
		UpdatedAt: time.Now(),
	}

	if m.StatusPageID != nil {
		if *m.StatusPageID == "" {
			update.StatusPageID = nil
		} else {
			id, err := primitive.ObjectIDFromHex(*m.StatusPageID)
			if err != nil {
				return nil, err
			}
			update.StatusPageID = &id
		}
	}

	return update, nil
}

type MongoRepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("incidents")
	return &MongoRepositoryImpl{client, db, collection}
}

func (r *MongoRepositoryImpl) Create(ctx context.Context, incident *Model) (*Model, error) {
	incident.CreatedAt = time.Now()
	incident.UpdatedAt = time.Now()

	mongoIncident, err := toMongoModel(incident)
	if err != nil {
		return nil, err
	}

	result, err := r.collection.InsertOne(ctx, mongoIncident)
	if err != nil {
		return nil, err
	}

	mongoIncident.ID = result.InsertedID.(primitive.ObjectID)
	return toDomainModel(mongoIncident), nil
}

func (r *MongoRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	var mongoIncident mongoModel
	err = r.collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&mongoIncident)
	if err != nil {
		return nil, err
	}

	return toDomainModel(&mongoIncident), nil
}

func (r *MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	filter := bson.M{}

	if q != "" {
		searchTerm := strings.ToLower(q)
		filter = bson.M{
			"$or": []bson.M{
				{"title": bson.M{"$regex": searchTerm, "$options": "i"}},
				{"content": bson.M{"$regex": searchTerm, "$options": "i"}},
			},
		}
	}

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	if page > 0 {
		skip := int64(page * limit)
		opts.SetSkip(skip)
	}

	if limit > 0 {
		opts.SetLimit(int64(limit))
	}

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var mongoIncidents []mongoModel
	err = cursor.All(ctx, &mongoIncidents)
	if err != nil {
		return nil, err
	}

	incidents := make([]*Model, len(mongoIncidents))
	for i, mongoIncident := range mongoIncidents {
		incidents[i] = toDomainModel(&mongoIncident)
	}

	return incidents, nil
}

func (r *MongoRepositoryImpl) FindByStatusPageID(ctx context.Context, statusPageID string) ([]*Model, error) {
	statusPageObjID, err := primitive.ObjectIDFromHex(statusPageID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{
		"status_page_id": statusPageObjID,
		"active":         true,
	}

	opts := options.Find().SetSort(bson.D{
		{Key: "pin", Value: -1},
		{Key: "created_at", Value: -1},
	})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var mongoIncidents []mongoModel
	err = cursor.All(ctx, &mongoIncidents)
	if err != nil {
		return nil, err
	}

	incidents := make([]*Model, len(mongoIncidents))
	for i, mongoIncident := range mongoIncidents {
		incidents[i] = toDomainModel(&mongoIncident)
	}

	return incidents, nil
}

func (r *MongoRepositoryImpl) Update(ctx context.Context, id string, incident *UpdateModel) (*Model, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	mongoUpdate, err := toMongoUpdateModel(incident)
	if err != nil {
		return nil, err
	}

	update := bson.M{"$set": mongoUpdate}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": objID}, update)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

func (r *MongoRepositoryImpl) Delete(ctx context.Context, id string) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	_, err = r.collection.DeleteOne(ctx, bson.M{"_id": objID})
	return err
}
