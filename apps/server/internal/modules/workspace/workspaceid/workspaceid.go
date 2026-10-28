// Package workspaceid holds the type of a workspace's ID and nothing else, so that packages the
// probe links (the shared types) can name it without linking the workspace package and the storage
// it imports. workspace.ID is this type.
package workspaceid

// ID is the ID of a workspace. It is a type of its own so that it can't be swapped with the string
// ID of a record that a workspace owns.
type ID string
