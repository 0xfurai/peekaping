package websocket_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

var (
	okAnswer      = map[string]string{"status": "ok"}
	refusedAnswer = map[string]string{"status": "error", "message": "not allowed"}
)

// A leave is answered ok for a room a client could have joined, whether or not it did, and refused
// for a name that is no room of theirs to leave. Only the rooms a client joins can be left.
func TestLeaveRoomAnswersOnlyForTheRoomsAClientJoins(t *testing.T) {
	h := newHarness(t)
	a := h.connectAs(t, h.userA)
	a.joinOK(t, roomOf(workspaceA))

	for name, room := range map[string]string{
		"a monitor ID that isn't a UUID": "monitor:abc",
		"a workspace ID that isn't one":  "workspace:abc",
		"no ID":                          "workspace:",
		"the old install-wide room":      "monitor:all",
		"the install room":               "install",
		"an unknown prefix":              "team:" + string(workspaceA),
		"an empty name":                  "",
		"a prefix in capitals":           "MONITOR:" + monitorA2,
		"a name too long to be a room":   "monitor:" + strings.Repeat("a", 200),
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			assert.Equal(t, refusedAnswer, a.emitWithAck(t, "leave_room", room))
		})
	}
	for name, room := range map[string]string{
		"a room the client is not in":          "monitor:" + monitorB,
		"a room that does not exist":           "monitor:" + uuid.NewString(),
		"a workspace of another":               roomOf(workspaceB),
		"a monitor room with a capital letter": "monitor:" + strings.ToUpper(monitorA2),
	} {
		t.Run("answers "+name, func(t *testing.T) {
			assert.Equal(t, okAnswer, a.emitWithAck(t, "leave_room", room))
		})
	}

	// The refused names left nothing: the client is still in the room it joined
	publishHeartbeat(t, h.bus, monitorA, "a-1")
	a.untilMsg(t, "a-1")
}

// A leave names the room in any case of its ID, and takes the client out of the room the join made
func TestLeaveRoomTakesAnIDInCapitals(t *testing.T) {
	h := newHarness(t)
	a := h.connectAs(t, h.userA)
	a.joinOK(t, "monitor:"+monitorA)
	a.joinOK(t, "monitor:"+monitorA2)

	assert.Equal(t, okAnswer, a.emitWithAck(t, "leave_room", "monitor:"+strings.ToUpper(monitorA)))

	// The leave is done once it is answered: A's heartbeat is not delivered, A2's is
	publishHeartbeat(t, h.bus, monitorA, "a-1")
	publishHeartbeat(t, h.bus, monitorA2, "a2-1")
	assert.Equal(t, []string{"a2-1"}, heartbeatMsgs(a.untilMsg(t, "a2-1")))
}

// failingFind is a monitor repository that can't tell which workspace a monitor is in
type failingFind struct {
	monitor.MonitorRepository
	unreadable string
}

func (f failingFind) FindWorkspaceIDAnyWorkspace(ctx context.Context, id string) (workspace.ID, bool, error) {
	if id == f.unreadable {
		return "", false, errors.New("database is closed")
	}
	return f.MonitorRepository.FindWorkspaceIDAnyWorkspace(ctx, id)
}

// A heartbeat goes to its monitor's room first. It reaches the workspace's room when the monitor's
// workspace is known, and when it can't be read, or the monitor is gone, it goes no further and
// the next heartbeat is handled as usual.
func TestAHeartbeatStopsAtTheMonitorRoomWhenItsWorkspaceIsUnknown(t *testing.T) {
	gone, unreadable := uuid.NewString(), uuid.NewString()
	h := newHarnessWith(t, 0, 0, func(r monitor.MonitorRepository) monitor.MonitorRepository {
		return failingFind{MonitorRepository: r, unreadable: unreadable}
	})
	a := h.connectAs(t, h.userA)
	a.joinOK(t, roomOf(workspaceA))

	publishHeartbeat(t, h.bus, gone, "gone-1")
	publishHeartbeat(t, h.bus, unreadable, "unreadable-1")
	publishHeartbeat(t, h.bus, monitorA, "a-1")

	seen := heartbeatMsgs(a.untilMsg(t, "a-1"))
	assert.Equal(t, []string{"a-1"}, seen, "the workspace room got none of the two")
}
