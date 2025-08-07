package bruteforce

import (
	"context"
	"peekaping/src/config"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// mongoModel represents the MongoDB document for login state
type mongoModel struct {
	Key         string     `bson:"_id"` // Use key as the document ID for uniqueness
	FailCount   int        `bson:"fail_count"`
	FirstFailAt time.Time  `bson:"first_fail_at"`
	LockedUntil *time.Time `bson:"locked_until,omitempty"`
}

func toDomainModelFromMongo(mm *mongoModel) *Model {
	return &Model{
		Key:         mm.Key,
		FailCount:   mm.FailCount,
		FirstFailAt: mm.FirstFailAt,
		LockedUntil: mm.LockedUntil,
	}
}

func toMongoModel(m *Model) *mongoModel {
	return &mongoModel{
		Key:         m.Key,
		FailCount:   m.FailCount,
		FirstFailAt: m.FirstFailAt,
		LockedUntil: m.LockedUntil,
	}
}

type MongoRepositoryImpl struct {
	client               *mongo.Client
	db                   *mongo.Database
	loginStateCollection *mongo.Collection
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	loginStateCollection := db.Collection("login_state")

	repo := &MongoRepositoryImpl{
		client:               client,
		db:                   db,
		loginStateCollection: loginStateCollection,
	}

	// Create indexes for better performance
	repo.createIndexes()

	return repo
}

func (r *MongoRepositoryImpl) createIndexes() {
	// Create indexes for better performance on login_state collection
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "locked_until", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0), // Auto-expire when locked_until time is reached
		},
		{
			Keys:    bson.D{{Key: "first_fail_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(int32(24 * time.Hour / time.Second)), // Auto-expire old states after 24 hours
		},
	}

	r.loginStateCollection.Indexes().CreateMany(context.Background(), indexes)
}

// FindByKey retrieves login state by key
func (r *MongoRepositoryImpl) FindByKey(ctx context.Context, key string) (*Model, error) {
	var mm mongoModel
	err := r.loginStateCollection.FindOne(ctx, bson.M{"_id": key}).Decode(&mm)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return toDomainModelFromMongo(&mm), nil
}

// Create creates a new login state record
func (r *MongoRepositoryImpl) Create(ctx context.Context, model *Model) (*Model, error) {
	mm := toMongoModel(model)
	_, err := r.loginStateCollection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}
	return toDomainModelFromMongo(mm), nil
}

// Update updates an existing login state record
func (r *MongoRepositoryImpl) Update(ctx context.Context, key string, updateModel *UpdateModel) error {
	update := bson.M{}
	if updateModel.FailCount != nil {
		update["fail_count"] = *updateModel.FailCount
	}
	if updateModel.FirstFailAt != nil {
		update["first_fail_at"] = *updateModel.FirstFailAt
	}
	if updateModel.LockedUntil != nil {
		update["locked_until"] = *updateModel.LockedUntil
	}

	_, err := r.loginStateCollection.UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": update})
	return err
}

// Delete removes a login state record
func (r *MongoRepositoryImpl) Delete(ctx context.Context, key string) error {
	_, err := r.loginStateCollection.DeleteOne(ctx, bson.M{"_id": key})
	return err
}

// IsLocked checks if a key is currently locked
func (r *MongoRepositoryImpl) IsLocked(ctx context.Context, key string) (bool, time.Time, error) {
	var mm mongoModel
	filter := bson.M{
		"_id":          key,
		"locked_until": bson.M{"$gt": time.Now()},
	}

	err := r.loginStateCollection.FindOne(ctx, filter).Decode(&mm)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, time.Time{}, nil
		}
		return false, time.Time{}, err
	}

	if mm.LockedUntil != nil {
		return true, *mm.LockedUntil, nil
	}

	return false, time.Time{}, nil
}

// OnFailure atomically handles failure logic with window and locking
func (r *MongoRepositoryImpl) OnFailure(ctx context.Context, key string, now time.Time, window time.Duration, max int, lockout time.Duration) (bool, time.Time, error) {
	var locked bool
	var until time.Time

	session, err := r.client.StartSession()
	if err != nil {
		return false, time.Time{}, err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sc mongo.SessionContext) (interface{}, error) {
		var mm mongoModel
		filter := bson.M{"_id": key}

		err := r.loginStateCollection.FindOne(sc, filter).Decode(&mm)
		windowStart := now.Add(-window)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				// First failure for this key
				mm = mongoModel{
					Key:         key,
					FailCount:   1,
					FirstFailAt: now,
					LockedUntil: nil,
				}
				_, err = r.loginStateCollection.InsertOne(sc, mm)
				return nil, err
			}
			return nil, err
		}

		// Check if we're outside the window - reset if so
		if mm.FirstFailAt.Before(windowStart) {
			mm.FailCount = 1
			mm.FirstFailAt = now
			mm.LockedUntil = nil
		} else {
			// Within window, increment counter
			mm.FailCount++

			// Check if we need to lock
			if mm.FailCount >= max {
				lockUntil := now.Add(lockout)
				mm.LockedUntil = &lockUntil
				locked = true
				until = lockUntil
			}
		}

		update := bson.M{"$set": mm}
		_, err = r.loginStateCollection.UpdateOne(sc, filter, update)
		return nil, err
	})

	return locked, until, err
}
