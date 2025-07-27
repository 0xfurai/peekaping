package domain_status_page

import (
	"context"
	"peekaping/src/config"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoModel struct {
	ID           primitive.ObjectID `bson:"_id"`
	StatusPageID primitive.ObjectID `bson:"status_page_id"`
	Domain       string             `bson:"domain"`
	CreatedAt    time.Time          `bson:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at"`
}

type mongoUpdateModel struct {
	StatusPageID *primitive.ObjectID `bson:"status_page_id,omitempty"`
	Domain       *string             `bson:"domain,omitempty"`
	UpdatedAt    *time.Time          `bson:"updated_at,omitempty"`
}

func toDomainModelFromMongo(mm *mongoModel) *Model {
	return &Model{
		ID:           mm.ID.Hex(),
		StatusPageID: mm.StatusPageID.Hex(),
		Domain:       mm.Domain,
		CreatedAt:    mm.CreatedAt,
		UpdatedAt:    mm.UpdatedAt,
	}
}

type MongoRepositoryImpl struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("domain_status_page")
	return &MongoRepositoryImpl{client, db, collection}
}

func (r *MongoRepositoryImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	statusPageObjectID, err := primitive.ObjectIDFromHex(entity.StatusPageID)
	if err != nil {
		return nil, err
	}

	mm := &mongoModel{
		ID:           primitive.NewObjectID(),
		StatusPageID: statusPageObjectID,
		Domain:       entity.Domain,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	_, err = r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromMongo(mm), nil
}

func (r *MongoRepositoryImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"_id": objectID}
	var mm mongoModel
	err = r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromMongo(&mm), nil
}

func (r *MongoRepositoryImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	filter := bson.M{}
	if q != "" {
		filter["domain"] = bson.M{"$regex": q, "$options": "i"}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(page * limit))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var mms []*mongoModel
	err = cursor.All(ctx, &mms)
	if err != nil {
		return nil, err
	}

	var models []*Model
	for _, mm := range mms {
		models = append(models, toDomainModelFromMongo(mm))
	}
	return models, nil
}

func (r *MongoRepositoryImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	statusPageObjectID, err := primitive.ObjectIDFromHex(entity.StatusPageID)
	if err != nil {
		return nil, err
	}

	update := bson.M{
		"status_page_id": statusPageObjectID,
		"domain":         entity.Domain,
		"updated_at":     time.Now().UTC(),
	}

	filter := bson.M{"_id": objectID}
	updateDoc := bson.M{"$set": update}

	_, err = r.collection.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

func (r *MongoRepositoryImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	update := mongoUpdateModel{
		UpdatedAt: &[]time.Time{time.Now().UTC()}[0],
	}

	if entity.StatusPageID != nil {
		statusPageObjectID, err := primitive.ObjectIDFromHex(*entity.StatusPageID)
		if err != nil {
			return nil, err
		}
		update.StatusPageID = &statusPageObjectID
	}

	if entity.Domain != nil {
		update.Domain = entity.Domain
	}

	filter := bson.M{"_id": objectID}
	updateDoc := bson.M{"$set": update}

	_, err = r.collection.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		return nil, err
	}

	return r.FindByID(ctx, id)
}

func (r *MongoRepositoryImpl) Delete(ctx context.Context, id string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objectID}
	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}

// Additional methods for managing relationships
func (r *MongoRepositoryImpl) AddDomainToStatusPage(ctx context.Context, statusPageID, domain string) (*Model, error) {
	// Check if the relationship already exists
	existing, err := r.FindByStatusPageAndDomain(ctx, statusPageID, domain)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Update existing relationship
		return r.UpdatePartial(ctx, existing.ID, &PartialUpdateDto{})
	}

	statusPageObjectID, err := primitive.ObjectIDFromHex(statusPageID)
	if err != nil {
		return nil, err
	}

	// Create new relationship
	mm := &mongoModel{
		ID:           primitive.NewObjectID(),
		StatusPageID: statusPageObjectID,
		Domain:       domain,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	_, err = r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toDomainModelFromMongo(mm), nil
}

func (r *MongoRepositoryImpl) RemoveDomainFromStatusPage(ctx context.Context, statusPageID, domain string) error {
	statusPageObjectID, err := primitive.ObjectIDFromHex(statusPageID)
	if err != nil {
		return err
	}

	filter := bson.M{
		"status_page_id": statusPageObjectID,
		"domain":         domain,
	}

	_, err = r.collection.DeleteOne(ctx, filter)
	return err
}

func (r *MongoRepositoryImpl) GetDomainsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error) {
	statusPageObjectID, err := primitive.ObjectIDFromHex(statusPageID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"status_page_id": statusPageObjectID}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var mms []*mongoModel
	err = cursor.All(ctx, &mms)
	if err != nil {
		return nil, err
	}

	var models []*Model
	for _, mm := range mms {
		models = append(models, toDomainModelFromMongo(mm))
	}
	return models, nil
}

func (r *MongoRepositoryImpl) FindByStatusPageAndDomain(ctx context.Context, statusPageID, domain string) (*Model, error) {
	statusPageObjectID, err := primitive.ObjectIDFromHex(statusPageID)
	if err != nil {
		return nil, err
	}

	filter := bson.M{
		"status_page_id": statusPageObjectID,
		"domain":         domain,
	}

	var mm mongoModel
	err = r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromMongo(&mm), nil
}

func (r *MongoRepositoryImpl) DeleteAllDomainsForStatusPage(ctx context.Context, statusPageID string) error {
	statusPageObjectID, err := primitive.ObjectIDFromHex(statusPageID)
	if err != nil {
		return err
	}

	filter := bson.M{"status_page_id": statusPageObjectID}
	_, err = r.collection.DeleteMany(ctx, filter)
	return err
}

func (r *MongoRepositoryImpl) FindByDomain(ctx context.Context, domain string) (*Model, error) {
	filter := bson.M{
		"domain": domain,
	}

	var mm mongoModel
	err := r.collection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromMongo(&mm), nil
}
