package websocket

import (
	"context"
	"fmt"
	"net/http"
	"peekaping/internal/config"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/workspace"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zishang520/socket.io/v2/socket"
	"go.uber.org/zap"
)

// The rooms a socket can be in, and the events they deliver. Rooms are server-side: a client asks
// for a room with join_room, and the server lets it into the two kinds below only while its user is a
// member of the workspace they belong to.
//
//   - workspace:<id> gets "workspace:<id>:heartbeat" for every monitor of the workspace.
//   - monitor:<id> gets "monitor:<id>:heartbeat" for that monitor.
//
// Every signed-in socket is in the install room, which gets what is about the install and carries
// no workspace's records: "probe:status", a probe going offline or coming back.
const (
	workspacePrefix = "workspace:"
	monitorPrefix   = "monitor:"
	installRoom     = "install"
	// probeStatusEvent is the event the install room gets when a probe goes offline or comes back
	probeStatusEvent = "probe:status"
	// maxRoomName bounds what a client may make the server look up
	maxRoomName = 128
)

type Server struct {
	io         *socket.Server
	eventBus   events.EventBus
	tokenMaker *auth.TokenMaker
	workspaces workspace.Repository
	monitors   monitor.MonitorRepository
	logger     *zap.SugaredLogger
	// onConnection runs first in the connection handler. Only tests set it, to slow the handler.
	onConnection func()

	// monitorWorkspaces remembers the workspace of each monitor that has had a heartbeat or a
	// join, so that a heartbeat costs no read: a monitor never changes workspace. A deleted
	// monitor is forgotten.
	monitorWorkspaces sync.Map // monitor ID -> workspace.ID

	// sockets are the open sockets, by user, for taking a user out of a workspace's rooms
	socketsMu sync.Mutex
	sockets   map[string]map[socket.SocketId]*userSocket
}

// userSocket is an open socket and the lock that keeps its membership check and its join together
// against it being taken out of a workspace.
type userSocket struct {
	client *socket.Socket
	userID string
	// mu is held while a join checks the membership and enters the room, and while the socket
	// leaves a workspace's rooms. A removal commits before MemberLeft is published, so a join that
	// passed its check before the removal is finished, and then left, before the leave returns.
	mu sync.Mutex
}

type SocketData struct {
	UserId string
}

func NewServer(
	cfg *config.Config,
	eventBus events.EventBus,
	tokenMaker *auth.TokenMaker,
	workspaces workspace.Repository,
	monitors monitor.MonitorRepository,
	logger *zap.SugaredLogger,
) (*Server, error) {
	opts := socket.DefaultServerOptions()
	io := socket.NewServer(nil, opts)

	server := &Server{
		io:         io,
		eventBus:   eventBus,
		tokenMaker: tokenMaker,
		workspaces: workspaces,
		monitors:   monitors,
		logger:     logger,
		sockets:    map[string]map[socket.SocketId]*userSocket{},
	}

	io.Use(func(s *socket.Socket, next func(*socket.ExtendedError)) {
		// The token comes in the auth payload of the socket.io CONNECT packet, not
		// in the query string, which proxies write to their access logs.
		auth, _ := s.Handshake().Auth.(map[string]any)
		access_token, ok := auth["token"].(string)
		if !ok || access_token == "" {
			next(socket.NewExtendedError("access_token is required", "test"))
			return
		}

		// Create context for token verification
		ctx := context.Background()
		claims, err := tokenMaker.VerifyToken(ctx, access_token, "access")
		if err != nil {
			next(socket.NewExtendedError("Unauthorized", nil))
			return
		}

		data := SocketData{UserId: fmt.Sprint(claims.UserID)}

		s.SetData(data)

		next(nil)
	})

	// The room handlers go in a middleware of their own, after the auth one, so only an
	// authenticated socket gets them. The library runs the middlewares before it sends CONNECT to
	// the client, and emits "connection" after: a handler registered there could miss a join_room
	// that the client sent as soon as it saw CONNECT.
	io.Use(func(client *socket.Socket, next func(*socket.ExtendedError)) {
		data, ok := client.Data().(SocketData)
		if !ok {
			next(socket.NewExtendedError("Unauthorized", nil))
			return
		}
		us := server.register(client, data.UserId)
		client.Join(socket.Room(installRoom))
		client.On("disconnect", safe(logger, "disconnect", func(...interface{}) { server.unregister(us) }))

		client.On("join_room", safe(logger, "join_room", func(args ...interface{}) {
			ack := ackOf(args)
			roomName, ok := firstString(args)
			if !ok {
				logger.Debugf("join_room: dropped, no room name in %d arguments", len(args))
				reply(ack, "error", "not allowed")
				return
			}
			if reason := server.join(us, roomName); reason != "" {
				logger.Debugf("join_room: refused %q for user %s: %s", roomName, us.userID, reason)
				reply(ack, "error", "not allowed")
				return
			}
			logger.Debugf("join_room: %s", roomName)
			reply(ack, "ok", "")
		}))

		client.On("leave_room", safe(logger, "leave_room", func(args ...interface{}) {
			ack := ackOf(args)
			roomName, ok := firstString(args)
			if !ok {
				logger.Debugf("leave_room: dropped, no room name in %d arguments", len(args))
				reply(ack, "error", "not allowed")
				return
			}
			// Only the rooms a client joins: it stays in the install room, and in its own
			if _, _, ok := parseRoom(roomName); !ok {
				logger.Debugf("leave_room: ignored %q", roomName)
				reply(ack, "error", "not allowed")
				return
			}
			logger.Debugf("leave_room: %s", roomName)
			us.mu.Lock()
			client.Leave(socket.Room(roomName))
			us.mu.Unlock()
			reply(ack, "ok", "")
		}))

		next(nil)
	})

	io.On("connection", safe(logger, "connection", func(clients ...interface{}) {
		if server.onConnection != nil {
			server.onConnection()
		}
		var client *socket.Socket
		if len(clients) > 0 {
			client, _ = clients[0].(*socket.Socket)
		}
		if client == nil {
			logger.Warnf("[WS]connection: dropped, no socket in %d arguments", len(clients))
			return
		}
		// The auth middleware sets the data, so a socket without it never authenticated
		data, ok := client.Data().(SocketData)
		if !ok {
			logger.Warn("[WS]connection: disconnecting a socket without user data")
			client.Disconnect(true)
			return
		}

		logger.Debugf("[WS]connection: %s", data.UserId)
	}))

	subscriber := events.NewSubscriber(eventBus, "websocket")

	// A heartbeat goes to its workspace's room and to its monitor's room, and to no other
	events.Subscribe(subscriber, events.HeartbeatEvent, func(ctx context.Context, hb heartbeat.Model) {
		monitorRoom := monitorPrefix + hb.MonitorID
		server.io.To(socket.Room(monitorRoom)).Emit(monitorRoom+":heartbeat", &hb)

		workspaceID, found, err := server.workspaceOf(ctx, hb.MonitorID)
		if err != nil {
			logger.Warnw("Heartbeat not sent to its workspace: could not read the monitor's workspace", "monitor_id", hb.MonitorID, "error", err)
			return
		}
		if !found {
			logger.Debugf("Heartbeat not sent to a workspace: monitor %s is gone", hb.MonitorID)
			return
		}
		workspaceRoom := workspacePrefix + string(workspaceID)
		server.io.To(socket.Room(workspaceRoom)).Emit(workspaceRoom+":heartbeat", &hb)
	})

	events.Subscribe(subscriber, events.MonitorDeleted, func(_ context.Context, monitorID string) {
		server.monitorWorkspaces.Delete(monitorID)
	})

	// A probe belongs to the install, so its status goes to every signed-in socket, in the install
	// room. The event names the probe and carries no workspace's records.
	//
	// On a cluster, the bus hands these two topics to one api replica only (the notification queue
	// takes them), so only the sockets on that replica hear of a probe.
	events.Subscribe(subscriber, probe.ProbeOffline, func(_ context.Context, e probe.Event) {
		server.sendProbeStatus("offline", e)
	})
	events.Subscribe(subscriber, probe.ProbeOnline, func(_ context.Context, e probe.Event) {
		server.sendProbeStatus("online", e)
	})

	events.Subscribe(subscriber, workspace.MemberLeft, server.memberLeft)

	return server, nil
}

type probeStatus struct {
	ProbeID   string    `json:"probe_id"`
	ProbeName string    `json:"probe_name"`
	Status    string    `json:"status"`
	Since     time.Time `json:"since"`
}

func (s *Server) sendProbeStatus(status string, e probe.Event) {
	s.io.To(socket.Room(installRoom)).Emit(probeStatusEvent, &probeStatus{ProbeID: e.ProbeID, ProbeName: e.ProbeName, Status: status, Since: e.Since})
}

func (s *Server) register(client *socket.Socket, userID string) *userSocket {
	us := &userSocket{client: client, userID: userID}
	s.socketsMu.Lock()
	defer s.socketsMu.Unlock()
	if s.sockets[userID] == nil {
		s.sockets[userID] = map[socket.SocketId]*userSocket{}
	}
	s.sockets[userID][client.Id()] = us
	return us
}

func (s *Server) unregister(us *userSocket) {
	s.socketsMu.Lock()
	defer s.socketsMu.Unlock()
	delete(s.sockets[us.userID], us.client.Id())
	if len(s.sockets[us.userID]) == 0 {
		delete(s.sockets, us.userID)
	}
}

func (s *Server) socketsOf(userID string) []*userSocket {
	s.socketsMu.Lock()
	defer s.socketsMu.Unlock()
	out := make([]*userSocket, 0, len(s.sockets[userID]))
	for _, us := range s.sockets[userID] {
		out = append(out, us)
	}
	return out
}

// parseRoom splits a room name a client may ask for into its prefix and its ID.
func parseRoom(name string) (prefix, id string, ok bool) {
	if len(name) > maxRoomName {
		return "", "", false
	}
	for _, p := range []string{workspacePrefix, monitorPrefix} {
		if id, found := strings.CutPrefix(name, p); found && id != "" {
			return p, id, true
		}
	}
	return "", "", false
}

// workspaceOf returns the workspace a monitor is in, and false when there is no such monitor.
func (s *Server) workspaceOf(ctx context.Context, monitorID string) (workspace.ID, bool, error) {
	if known, ok := s.monitorWorkspaces.Load(monitorID); ok {
		return known.(workspace.ID), true, nil
	}
	id, found, err := s.monitors.FindWorkspaceIDAnyWorkspace(ctx, monitorID)
	if err != nil || !found {
		return "", false, err
	}
	s.monitorWorkspaces.Store(monitorID, id)
	return id, true, nil
}

// join lets the socket into the room when its user is a member of the room's workspace, and
// returns why not when they aren't. A failure to read the membership refuses the join.
func (s *Server) join(us *userSocket, roomName string) (refusal string) {
	prefix, id, ok := parseRoom(roomName)
	if !ok {
		return "not a workspace or monitor room"
	}
	ctx := context.Background()

	us.mu.Lock()
	defer us.mu.Unlock()

	var target workspace.ID
	switch prefix {
	case workspacePrefix:
		target = workspace.ID(id)
	case monitorPrefix:
		workspaceID, found, err := s.workspaceOf(ctx, id)
		if err != nil {
			s.logger.Warnw("join_room refused: could not read the monitor's workspace", "monitor_id", id, "error", err)
			return "could not read the monitor"
		}
		if !found {
			return "no such monitor"
		}
		target = workspaceID
	}

	memberships, err := s.workspaces.ListMemberships(ctx, us.userID)
	if err != nil {
		s.logger.Warnw("join_room refused: could not read the user's memberships", "user_id", us.userID, "error", err)
		return "could not read the memberships"
	}
	if !slices.ContainsFunc(memberships, func(m workspace.Membership) bool { return m.Workspace.ID == target }) {
		return "not a member of the workspace"
	}
	us.client.Join(socket.Room(roomName))
	return ""
}

// memberLeft takes every open socket of the user out of the workspace's room and the rooms of its
// monitors, so that they stop getting its updates with no reconnect.
func (s *Server) memberLeft(ctx context.Context, e workspace.MemberLeftEvent) {
	for _, us := range s.socketsOf(e.UserID) {
		s.leaveWorkspace(ctx, us, e.WorkspaceID)
	}
}

func (s *Server) leaveWorkspace(ctx context.Context, us *userSocket, id workspace.ID) {
	us.mu.Lock()
	defer us.mu.Unlock()
	us.client.Leave(socket.Room(workspacePrefix + string(id)))
	for _, room := range us.client.Rooms().Keys() {
		monitorID, ok := strings.CutPrefix(string(room), monitorPrefix)
		if !ok {
			continue
		}
		workspaceID, found, err := s.workspaceOf(ctx, monitorID)
		// A monitor that is gone has no updates left to hide, and one that can't be read is
		// left, since a socket must not keep a room it may have lost
		if err != nil || !found || workspaceID == id {
			us.client.Leave(room)
		}
	}
}

// ackOf returns the acknowledgement function a client attached to its message, or nil. The web app
// sends none; a client that wants an answer gets one.
func ackOf(args []interface{}) socket.Ack {
	if len(args) == 0 {
		return nil
	}
	ack, _ := args[len(args)-1].(socket.Ack)
	return ack
}

func reply(ack socket.Ack, status, message string) {
	if ack == nil {
		return
	}
	answer := map[string]string{"status": status}
	if message != "" {
		answer["message"] = message
	}
	ack([]interface{}{answer}, nil)
}

// safe makes a socket handler recover from a panic: it logs the panic with the event
// name and drops the message. engine.io runs each handler in its own goroutine with no
// recover, and the HTTP recovery middleware doesn't cover that goroutine, so a panic
// that gets out of a handler ends the process.
func safe(logger *zap.SugaredLogger, event string, fn func(args ...interface{})) func(args ...interface{}) {
	return func(args ...interface{}) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorw("Socket handler panicked",
					"event", event,
					"panic", r,
					"stack", string(debug.Stack()),
				)
			}
		}()
		fn(args...)
	}
}

// firstString returns the first argument of a client's message if it's a string.
// The arguments are client input, so they can be missing or of any type.
func firstString(args []interface{}) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	s, ok := args[0].(string)
	return s, ok
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.io.ServeHandler(nil).ServeHTTP(w, r)
}

func (s *Server) Close() {
	s.io.Close(nil)
}
