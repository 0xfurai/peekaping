package mongoimport

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Report says what the import did with every collection: how many documents it
// read, how many rows it wrote, and why it skipped or changed the rest.
type Report struct {
	Collections []*CollectionReport
	// Workspace is the workspace that the import made for the imported users, or nil when it made none
	Workspace *WorkspaceReport
	// Notes are things the SQL tables have no place for, which the import leaves out.
	Notes []string
}

// CollectionReport is the outcome for one MongoDB collection.
type CollectionReport struct {
	// Collection is the MongoDB collection and Table the SQL table it was copied into.
	Collection string
	Table      string
	Read       int
	Copied     int
	// Skipped counts the documents that were not copied, by reason.
	Skipped map[string]int
	// Fixed counts the rows that were copied with a value changed to fit, by what changed.
	Fixed map[string]int
}

// WorkspaceReport says what the import did to give the install its first workspace.
type WorkspaceReport struct {
	Name string
	// Owners is how many imported users it made owners and instance admins of the workspace.
	Owners int
}

func (r *Report) collection(collection, table string) *CollectionReport {
	c := &CollectionReport{Collection: collection, Table: table, Skipped: map[string]int{}, Fixed: map[string]int{}}
	r.Collections = append(r.Collections, c)
	return c
}

func (r *Report) note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

func (c *CollectionReport) skip(reason string) { c.Skipped[reason]++ }

func (c *CollectionReport) fix(what string) { c.Fixed[what]++ }

// SkippedTotal is how many documents of the collection were not copied.
func (c *CollectionReport) SkippedTotal() int {
	total := 0
	for _, n := range c.Skipped {
		total += n
	}
	return total
}

// Write prints the report as a table with a line for every reason a document
// was skipped or a value changed.
func (r *Report) Write(w io.Writer) {
	width := len("collection")
	for _, c := range r.Collections {
		width = max(width, len(c.Collection))
	}
	fmt.Fprintf(w, "%-*s  %-24s  %8s  %8s  %8s\n", width, "collection", "table", "read", "copied", "skipped")
	for _, c := range r.Collections {
		fmt.Fprintf(w, "%-*s  %-24s  %8d  %8d  %8d\n", width, c.Collection, c.Table, c.Read, c.Copied, c.SkippedTotal())
	}
	for _, c := range r.Collections {
		for _, reason := range sortedKeys(c.Skipped) {
			fmt.Fprintf(w, "skipped %s: %d %s\n", c.Collection, c.Skipped[reason], reason)
		}
		for _, what := range sortedKeys(c.Fixed) {
			fmt.Fprintf(w, "changed %s: %d %s\n", c.Collection, c.Fixed[what], what)
		}
	}
	if ws := r.Workspace; ws != nil {
		fmt.Fprintf(w, "workspace %s: made, with %d users as its owners and instance admins\n", ws.Name, ws.Owners)
	}
	for _, note := range r.Notes {
		fmt.Fprintf(w, "not kept: %s\n", note)
	}
}

// String is the report as Write prints it.
func (r *Report) String() string {
	var b strings.Builder
	r.Write(&b)
	return b.String()
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
