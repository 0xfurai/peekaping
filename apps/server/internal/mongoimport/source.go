package mongoimport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// source is the MongoDB database being copied.
type source struct {
	db *mongo.Database
}

// readBatchSize is how many documents a cursor fetches at a time.
const readBatchSize = 1000

// each calls fn for every document of a collection that matches filter, in
// the order given. The collection is read as raw documents, with this package's
// own accessors, and not through the modules' Mongo repositories: those create
// indexes on construction and decode into structs that drop the stray keys the
// import wants to see. A collection that doesn't exist has no documents.
func (s *source) each(ctx context.Context, collection string, sort bson.D, fn func(doc) error) error {
	opts := options.Find().SetBatchSize(readBatchSize).SetAllowDiskUse(true)
	if len(sort) > 0 {
		opts.SetSort(sort)
	}
	cursor, err := s.db.Collection(collection).Find(ctx, bson.D{}, opts)
	if err != nil {
		return fmt.Errorf("read %s: %w", collection, err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		// The cursor reuses its buffer, so a document that outlives this call needs a copy
		if err := fn(doc(append(bson.Raw(nil), cursor.Current...))); err != nil {
			return err
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("read %s: %w", collection, err)
	}
	return nil
}

// hasAny is whether any of the collections has a document. A collection that doesn't exist has none.
func (s *source) hasAny(ctx context.Context, collections ...string) (bool, error) {
	for _, collection := range collections {
		err := s.db.Collection(collection).FindOne(ctx, bson.D{}).Err()
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return false, fmt.Errorf("read %s: %w", collection, err)
		}
	}
	return false, nil
}

// doc is one MongoDB document. Its accessors are as forgiving as the MongoDB
// driver's struct decoding: a missing, null or mistyped field reads as zero.
type doc bson.Raw

func (d doc) value(key string) (bson.RawValue, bool) {
	v, err := bson.Raw(d).LookupErr(key)
	if err != nil || v.Type == bsontype.Null || v.Type == bsontype.Undefined {
		return bson.RawValue{}, false
	}
	return v, true
}

func (d doc) has(key string) bool {
	_, ok := d.value(key)
	return ok
}

// id is the document's _id, the one every row of the import is identified by.
func (d doc) id() (primitive.ObjectID, bool) {
	return d.objectID("_id")
}

// objectID reads an ObjectID, or the hex form of one that a field stores as a string.
func (d doc) objectID(key string) (primitive.ObjectID, bool) {
	v, ok := d.value(key)
	if !ok {
		return primitive.NilObjectID, false
	}
	if id, ok := v.ObjectIDOK(); ok {
		return id, true
	}
	if s, ok := v.StringValueOK(); ok {
		if id, err := primitive.ObjectIDFromHex(strings.TrimSpace(s)); err == nil {
			return id, true
		}
	}
	return primitive.NilObjectID, false
}

func (d doc) str(key string) string {
	s, _ := d.optStr(key)
	return s
}

func (d doc) optStr(key string) (string, bool) {
	v, ok := d.value(key)
	if !ok {
		return "", false
	}
	s, ok := v.StringValueOK()
	return s, ok
}

// strPtr is nil for a field that's missing, which SQL stores as NULL.
func (d doc) strPtr(key string) *string {
	if s, ok := d.optStr(key); ok {
		return &s
	}
	return nil
}

func (d doc) boolean(key string) bool {
	v, ok := d.value(key)
	if !ok {
		return false
	}
	b, _ := v.BooleanOK()
	return b
}

func (d doc) float(key string) float64 {
	v, ok := d.value(key)
	if !ok {
		return 0
	}
	switch v.Type {
	case bsontype.Double:
		return v.Double()
	case bsontype.Int32:
		return float64(v.Int32())
	case bsontype.Int64:
		return float64(v.Int64())
	}
	return 0
}

// integer reads a whole number stored as int32, int64 or double.
func (d doc) integer(key string) int64 {
	v, ok := d.value(key)
	if !ok {
		return 0
	}
	switch v.Type {
	case bsontype.Int32:
		return int64(v.Int32())
	case bsontype.Int64:
		return v.Int64()
	case bsontype.Double:
		if f := v.Double(); !math.IsNaN(f) && !math.IsInf(f, 0) {
			return int64(f)
		}
	}
	return 0
}

func (d doc) intPtr(key string) *int {
	if !d.has(key) {
		return nil
	}
	n := int(d.integer(key))
	return &n
}

func (d doc) int64Ptr(key string) *int64 {
	if !d.has(key) {
		return nil
	}
	n := d.integer(key)
	return &n
}

// ints reads an array of whole numbers.
func (d doc) ints(key string) []int {
	v, ok := d.value(key)
	if !ok {
		return nil
	}
	array, ok := v.ArrayOK()
	if !ok {
		return nil
	}
	values, err := array.Values()
	if err != nil {
		return nil
	}
	var out []int
	for _, item := range values {
		switch item.Type {
		case bsontype.Int32:
			out = append(out, int(item.Int32()))
		case bsontype.Int64:
			out = append(out, int(item.Int64()))
		case bsontype.Double:
			out = append(out, int(item.Double()))
		}
	}
	return out
}

// when reads a point in time stored as a date, or as an RFC 3339 string, which
// is how some updates of a maintenance window wrote it. The time is in UTC. A
// zero time, such as the one a full update writes for a field it doesn't set,
// reads as missing.
func (d doc) when(key string) (time.Time, bool) {
	v, ok := d.value(key)
	if !ok {
		return time.Time{}, false
	}
	var t time.Time
	switch v.Type {
	case bsontype.DateTime:
		t = v.Time()
	case bsontype.Timestamp:
		seconds, _ := v.Timestamp()
		t = time.Unix(int64(seconds), 0)
	case bsontype.String:
		s, _ := v.StringValueOK()
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
		if err != nil {
			return time.Time{}, false
		}
		t = parsed
	default:
		return time.Time{}, false
	}
	if t.IsZero() {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// whenPtr is when for a nullable column.
func (d doc) whenPtr(key string) *time.Time {
	if t, ok := d.when(key); ok {
		return &t
	}
	return nil
}
