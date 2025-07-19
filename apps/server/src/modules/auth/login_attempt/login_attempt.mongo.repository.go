package login_attempt

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

type loginAttemptMongoModel struct {
	ID          primitive.ObjectID `bson:"_id"`
	Email       string             `bson:"email"`
	IPAddress   string             `bson:"ip_address"`
	UserAgent   string             `bson:"user_agent"`
	Success     bool               `bson:"success"`
	AttemptedAt time.Time          `bson:"attempted_at"`
	CreatedAt   time.Time          `bson:"created_at"`
}

func toLoginAttemptDomainModelFromMongo(mm *loginAttemptMongoModel) *LoginAttempt {
	return &LoginAttempt{
		ID:          mm.ID.Hex(),
		Email:       mm.Email,
		IPAddress:   mm.IPAddress,
		UserAgent:   mm.UserAgent,
		Success:     mm.Success,
		AttemptedAt: mm.AttemptedAt,
		CreatedAt:   mm.CreatedAt,
	}
}

type LoginAttemptMongoRepository struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewLoginAttemptMongoRepository(client *mongo.Client, cfg *config.Config) LoginAttemptRepository {
	db := client.Database(cfg.DBName)
	collection := db.Collection("login_attempts")
	return &LoginAttemptMongoRepository{client, db, collection}
}

func (r *LoginAttemptMongoRepository) Create(ctx context.Context, attempt *LoginAttemptCreateModel) (*LoginAttempt, error) {
	now := time.Now()
	mm := &loginAttemptMongoModel{
		ID:          primitive.NewObjectID(),
		Email:       attempt.Email,
		IPAddress:   attempt.IPAddress,
		UserAgent:   attempt.UserAgent,
		Success:     attempt.Success,
		AttemptedAt: now,
		CreatedAt:   now,
	}

	_, err := r.collection.InsertOne(ctx, mm)
	if err != nil {
		return nil, err
	}

	return toLoginAttemptDomainModelFromMongo(mm), nil
}

func (r *LoginAttemptMongoRepository) GetFailedAttemptsByEmail(ctx context.Context, email string, since time.Time) ([]*LoginAttempt, error) {
	filter := bson.M{
		"email":        email,
		"success":      false,
		"attempted_at": bson.M{"$gte": since},
	}

	opts := options.Find().SetSort(bson.M{"attempted_at": -1})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var attempts []*loginAttemptMongoModel
	if err = cursor.All(ctx, &attempts); err != nil {
		return nil, err
	}

	result := make([]*LoginAttempt, len(attempts))
	for i, attempt := range attempts {
		result[i] = toLoginAttemptDomainModelFromMongo(attempt)
	}

	return result, nil
}

func (r *LoginAttemptMongoRepository) GetFailedAttemptsByIP(ctx context.Context, ipAddress string, since time.Time) ([]*LoginAttempt, error) {
	filter := bson.M{
		"ip_address":   ipAddress,
		"success":      false,
		"attempted_at": bson.M{"$gte": since},
	}

	opts := options.Find().SetSort(bson.M{"attempted_at": -1})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var attempts []*loginAttemptMongoModel
	if err = cursor.All(ctx, &attempts); err != nil {
		return nil, err
	}

	result := make([]*LoginAttempt, len(attempts))
	for i, attempt := range attempts {
		result[i] = toLoginAttemptDomainModelFromMongo(attempt)
	}

	return result, nil
}

func (r *LoginAttemptMongoRepository) GetConsecutiveFailedAttemptsByEmail(ctx context.Context, email string) ([]*LoginAttempt, error) {
	filter := bson.M{"email": email}
	opts := options.Find().SetSort(bson.M{"attempted_at": -1})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var attempts []*loginAttemptMongoModel
	if err = cursor.All(ctx, &attempts); err != nil {
		return nil, err
	}

	// Find consecutive failed attempts from the most recent
	var consecutiveFailures []*LoginAttempt
	for _, attempt := range attempts {
		if attempt.Success {
			break // Stop at the first successful login
		}
		consecutiveFailures = append(consecutiveFailures, toLoginAttemptDomainModelFromMongo(attempt))
	}

	return consecutiveFailures, nil
}

func (r *LoginAttemptMongoRepository) GetConsecutiveFailedAttemptsByIP(ctx context.Context, ipAddress string) ([]*LoginAttempt, error) {
	filter := bson.M{"ip_address": ipAddress}
	opts := options.Find().SetSort(bson.M{"attempted_at": -1})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var attempts []*loginAttemptMongoModel
	if err = cursor.All(ctx, &attempts); err != nil {
		return nil, err
	}

	// Find consecutive failed attempts from the most recent
	var consecutiveFailures []*LoginAttempt
	for _, attempt := range attempts {
		if attempt.Success {
			break // Stop at the first successful login
		}
		consecutiveFailures = append(consecutiveFailures, toLoginAttemptDomainModelFromMongo(attempt))
	}

	return consecutiveFailures, nil
}

func (r *LoginAttemptMongoRepository) DeleteOldAttempts(ctx context.Context, olderThan time.Time) error {
	filter := bson.M{
		"attempted_at": bson.M{"$lt": olderThan},
	}

	_, err := r.collection.DeleteMany(ctx, filter)
	return err
}

func (r *LoginAttemptMongoRepository) GetLastSuccessfulLogin(ctx context.Context, email string) (*LoginAttempt, error) {
	filter := bson.M{
		"email":   email,
		"success": true,
	}

	opts := options.FindOne().SetSort(bson.M{"attempted_at": -1})
	var attempt loginAttemptMongoModel
	err := r.collection.FindOne(ctx, filter, opts).Decode(&attempt)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}

	return toLoginAttemptDomainModelFromMongo(&attempt), nil
}

func (r *LoginAttemptMongoRepository) GetAttemptsCount(ctx context.Context, email, ipAddress string, since time.Time) (int64, error) {
	filter := bson.M{
		"attempted_at": bson.M{"$gte": since},
	}

	if email != "" {
		filter["email"] = email
	}

	if ipAddress != "" {
		filter["ip_address"] = ipAddress
	}

	count, err := r.collection.CountDocuments(ctx, filter)
	return count, err
}
