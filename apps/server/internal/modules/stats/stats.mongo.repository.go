package stats

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"peekaping/internal/config"
	"peekaping/internal/modules/shared"
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

	_, err := db.Collection("stat_minutely").Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "monitor_id", Value: 1},
			{Key: "timestamp", Value: 1},
		},
	})

	if err != nil {
		panic("Failed to create index on stat_minutely collection:" + err.Error())
	}

	_, err = db.Collection("stat_hourly").Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "monitor_id", Value: 1},
			{Key: "timestamp", Value: 1},
		},
	})

	if err != nil {
		panic("Failed to create index on stat_hourly collection:" + err.Error())
	}

	_, err = db.Collection("stat_daily").Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "monitor_id", Value: 1},
			{Key: "timestamp", Value: 1},
		},
	})

	if err != nil {
		panic("Failed to create index on stat_daily collection:" + err.Error())
	}

	_, err = db.Collection("stat_minutely").Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "monitor_id", Value: 1},
			{Key: "timestamp", Value: 1},
		},
	})

	if err != nil {
		panic("Failed to create index on stat_minutely collection:" + err.Error())
	}

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
		ProbeID:     shared.LocalProbeID, // a Mongo install has no remote probes
		Timestamp:   mm.Timestamp,
		Ping:        mm.Ping,
		PingMin:     mm.PingMin,
		PingMax:     mm.PingMax,
		PingCount:   mm.Up - mm.Maintenance, // up also counts maintenance checks, which have no response time
		Up:          mm.Up,
		Down:        mm.Down,
		Maintenance: mm.Maintenance,
	}
}

// statDocumentID derives the _id of a new stat document from its monitor and timestamp.
func statDocumentID(monitorID primitive.ObjectID, timestamp time.Time) primitive.ObjectID {
	h := sha256.New()
	h.Write(monitorID[:])
	_ = binary.Write(h, binary.BigEndian, timestamp.UnixMilli()) // BSON dates keep milliseconds
	var id primitive.ObjectID
	copy(id[:], h.Sum(nil))
	return id
}

// AddToStat adds stat's counts and response times to the document for its
// monitor and timestamp, and creates the document if there isn't one, in one
// update. Concurrent heartbeats in the same bucket therefore can't overwrite
// each other's counts.
func (r *MongoRepository) AddToStat(ctx context.Context, stat *Stat, period StatPeriod) error {
	monitorID, err := primitive.ObjectIDFromHex(stat.MonitorID)
	if err != nil {
		return fmt.Errorf("invalid monitorID: %w", err)
	}

	// Each field reads the document as it was before this update; a new
	// document has none of them yet, so they count as 0.
	old := func(field string) bson.M { return bson.M{"$ifNull": bson.A{"$" + field, 0}} }
	set := bson.M{
		// Two concurrent first writes to a bucket both try to insert this _id, so
		// the second fails instead of creating a second document for the bucket.
		"_id":         bson.M{"$ifNull": bson.A{"$_id", statDocumentID(monitorID, stat.Timestamp)}},
		"up":          bson.M{"$add": bson.A{old("up"), stat.Up}},
		"down":        bson.M{"$add": bson.A{old("down"), stat.Down}},
		"maintenance": bson.M{"$add": bson.A{old("maintenance"), stat.Maintenance}},
	}
	if stat.PingCount > 0 {
		// ping is the average response time; up also counts maintenance checks, which have none.
		oldPingCount := bson.M{"$subtract": bson.A{old("up"), old("maintenance")}}
		hadPing := bson.M{"$gt": bson.A{oldPingCount, 0}}
		set["ping"] = bson.M{"$divide": bson.A{
			bson.M{"$add": bson.A{bson.M{"$multiply": bson.A{old("ping"), oldPingCount}}, stat.Ping * float64(stat.PingCount)}},
			bson.M{"$add": bson.A{oldPingCount, stat.PingCount}},
		}}
		set["ping_min"] = bson.M{"$cond": bson.A{hadPing, bson.M{"$min": bson.A{"$ping_min", stat.PingMin}}, stat.PingMin}}
		set["ping_max"] = bson.M{"$cond": bson.A{hadPing, bson.M{"$max": bson.A{"$ping_max", stat.PingMax}}, stat.PingMax}}
	}

	coll := r.getStatCollection(period)
	filter := bson.M{"monitor_id": monitorID, "timestamp": stat.Timestamp}
	update := mongo.Pipeline{{{Key: "$set", Value: set}}}
	_, err = coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		// Another write created the document first; now the filter finds it.
		_, err = coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	}
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

func (r *MongoRepository) DeleteOlderThan(ctx context.Context, period StatPeriod, cutoff time.Time) (int64, error) {
	result, err := r.getStatCollection(period).DeleteMany(ctx, bson.M{"timestamp": bson.M{"$lt": cutoff}})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

func (r *MongoRepository) DeleteByMonitorID(ctx context.Context, monitorID string) error {
	objectID, err := primitive.ObjectIDFromHex(monitorID)
	if err != nil {
		return err
	}

	filter := bson.M{"monitor_id": objectID}

	// Delete from all stats collections
	periods := []StatPeriod{StatMinutely, StatHourly, StatDaily}
	for _, period := range periods {
		coll := r.getStatCollection(period)
		_, err = coll.DeleteMany(ctx, filter)
		if err != nil {
			return err
		}
	}

	return nil
}
