package mongoimport

import (
	"strconv"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Reasons that apply to every collection.
const reasonNoObjectID = "no ObjectID as _id"

// byID reads a collection in the order its documents were made.
var byID = bson.D{{Key: "_id", Value: 1}}

// idSet is the ObjectIDs of the documents that were copied, for the documents
// that refer to them. MongoDB has no cascading deletes, so its link collections
// hold documents whose ends were deleted, which the SQL foreign keys reject.
type idSet map[primitive.ObjectID]struct{}

func (s idSet) add(id primitive.ObjectID) { s[id] = struct{}{} }

func (s idSet) has(id primitive.ObjectID) bool {
	_, ok := s[id]
	return ok
}

// stamps returns the time a document was created and the time it was last updated.
//
// A time that is missing or zero falls back to a time that can be known: an
// ObjectID holds the time it was made, which is when the document was made, and
// a document that was never updated was updated when it was made. What leaves
// a time zero is a full update of the modules that don't carry it (maintenance
// windows, links to status pages), and settings, whose upserts never set it.
func stamps(report *CollectionReport, d doc, id primitive.ObjectID, createdKey, updatedKey string) (created, updated time.Time) {
	created, ok := d.when(createdKey)
	if !ok {
		created = id.Timestamp().UTC()
		report.fix(createdKey + " missing or zero: used the time the ObjectID was made")
	}
	updated, ok = d.when(updatedKey)
	if !ok {
		updated = created
		report.fix(updatedKey + " missing or zero: used the creation time")
	}
	return created, updated
}

// uniqueText returns the text to store in a column of at most width characters
// that must be unique, as the first value that is not in taken, and adds it to taken.
//
// PostgreSQL enforces the width, so the text is cut to it, and values that differ only after the
// width then collide. A copied row is never dropped for that: the later value keeps its first
// characters and ends in ~2, ~3 and so on, which the report counts. SQLite keeps text whole, so
// there values collide only if they were equal, and the callers skip those first.
func (c *copier) uniqueText(report *CollectionReport, column string, taken map[string]bool, value string, width int) string {
	if c.cutsText && utf8.RuneCountInString(value) > width {
		value = string([]rune(value)[:width])
		report.fix(column + " cut to " + strconv.Itoa(width) + " characters")
	}
	candidate := value
	for n := 2; taken[candidate]; n++ {
		suffix := "~" + strconv.Itoa(n)
		runes := []rune(value)
		if len(runes)+len(suffix) > width {
			runes = runes[:width-len(suffix)]
		}
		candidate = string(runes) + suffix
		if !taken[candidate] {
			report.fix(column + " renamed to stay unique after being cut to " + strconv.Itoa(width) + " characters")
		}
	}
	taken[candidate] = true
	return candidate
}
