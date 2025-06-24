package stats

import (
	"context"
	"fmt"
	"peekaping/src/config"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoRepository struct {
	client *mongo.Client
	db     *mongo.Database
}

func NewMongoRepository(client *mongo.Client, cfg *config.Config) Repository {
	db := client.Database(cfg.DBName)
	return &MongoRepository{client, db}
}

func (r *MongoRepository) getStatCollection(period StatPeriod) *mongo.Collection {
	switch period {
	case StatMinutely:
		return r.db.Collection("stat_minutely")
	case StatHourly:
		return r.db.Collection("stat_hourly")
	case StatDaily:
		return r.db.Collection("stat_daily")
	default:
		return r.db.Collection("stat_minutely")
	}
}

type mongoModel struct {
	ID          primitive.ObjectID `bson:"_id"`
	MonitorID   primitive.ObjectID `bson:"monitor_id"`
	Timestamp   time.Time          `bson:"timestamp"`
	Ping        float64            `bson:"ping"`
	PingMin     float64            `bson:"ping_min"`
	PingMax     float64            `bson:"ping_max"`
	Up          int                `bson:"up"`
	Down        int                `bson:"down"`
	Maintenance int                `bson:"maintenance"`
}

func toDomainModel(mm *mongoModel) *Stat {
	return &Stat{
		ID:          mm.ID.Hex(),
		MonitorID:   mm.MonitorID.Hex(),
		Timestamp:   mm.Timestamp,
		Ping:        mm.Ping,
		PingMin:     mm.PingMin,
		PingMax:     mm.PingMax,
		Up:          mm.Up,
		Down:        mm.Down,
		Maintenance: mm.Maintenance,
	}
}

func toMongoModel(s *Stat) (*mongoModel, error) {
	objectID, err := primitive.ObjectIDFromHex(s.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid statID: %w", err)
	}
	monitorID, err := primitive.ObjectIDFromHex(s.MonitorID)
	if err != nil {
		return nil, fmt.Errorf("invalid monitorID: %w", err)
	}

	return &mongoModel{
		ID:          objectID,
		MonitorID:   monitorID,
		Timestamp:   s.Timestamp,
		Ping:        s.Ping,
		PingMin:     s.PingMin,
		PingMax:     s.PingMax,
		Up:          s.Up,
		Down:        s.Down,
		Maintenance: s.Maintenance,
	}, nil
}

func (r *MongoRepository) GetOrCreateStat(
	ctx context.Context,
	monitorID string,
	timestamp time.Time,
	period StatPeriod,
) (*Stat, error) {
	objectID, err := primitive.ObjectIDFromHex(monitorID)
	if err != nil {
		return nil, fmt.Errorf("invalid monitorID: %w", err)
	}
	coll := r.getStatCollection(period)
	filter := bson.M{"monitor_id": objectID, "timestamp": timestamp}
	var mm mongoModel
	err = coll.FindOne(ctx, filter).Decode(&mm)

	if err == mongo.ErrNoDocuments {
		mm = mongoModel{
			ID:        primitive.NewObjectID(),
			MonitorID: objectID,
			Timestamp: timestamp,
			Ping:      0,
			PingMin:   0,
			PingMax:   0,
			Up:        0,
			Down:      0,
		}
		return toDomainModel(&mm), nil
	} else if err != nil {
		return nil, err
	}
	return toDomainModel(&mm), nil
}

func (r *MongoRepository) UpsertStat(ctx context.Context, stat *Stat, period StatPeriod) error {
	coll := r.getStatCollection(period)
	mm, err := toMongoModel(stat)
	if err != nil {
		return err
	}

	filter := bson.M{"monitor_id": mm.MonitorID, "timestamp": mm.Timestamp}
	update :=
		bson.M{
			"$set": bson.M{
				"ping":        mm.Ping,
				"ping_min":    mm.PingMin,
				"ping_max":    mm.PingMax,
				"up":          mm.Up,
				"down":        mm.Down,
				"maintenance": mm.Maintenance,
			},
		}
	_, err = coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *MongoRepository) FindStatsByMonitorIDAndTimeRange(
	ctx context.Context,
	monitorID string,
	since,
	until time.Time,
	period StatPeriod,
) ([]*Stat, error) {
	objectID, err := primitive.ObjectIDFromHex(monitorID)
	if err != nil {
		return nil, fmt.Errorf("invalid monitorID: %w", err)
	}
	coll := r.getStatCollection(period)
	filter := bson.M{
		"monitor_id": objectID,
		"timestamp":  bson.M{"$gte": since, "$lte": until},
	}
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}})
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var stats []*Stat
	for cursor.Next(ctx) {
		var mm mongoModel
		if err := cursor.Decode(&mm); err != nil {
			return nil, err
		}
		stats = append(stats, toDomainModel(&mm))
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return stats, nil
}
