package workspace

import "peekaping/internal/modules/events"

// MemberLeftEvent is the payload of MemberLeft.
type MemberLeftEvent struct {
	WorkspaceID ID     `json:"workspace_id"`
	UserID      string `json:"user_id"`
}

// MemberLeft is published once a user's membership of a workspace is gone: after they left it, or
// were removed from it, and the change is stored. The websocket takes the user's open sockets out
// of the workspace's rooms. It belongs to no monitor, so it is handled with no order relative to
// other events.
//
// The name is the topic's name on the wire, so keep it as it is.
var MemberLeft = events.NewTopic("workspace.member.left", func(MemberLeftEvent) string { return "" })
