package websocket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"peekaping/internal/config"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/workspace"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zishang520/engine.io/v2/types"
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
// The ID in a room name is a UUID, and the room is named by its lower-case form.
const (
	workspacePrefix = "workspace:"
	monitorPrefix   = "monitor:"
	// maxRoomName bounds what a client may make the server look up
	maxRoomName = 128
	// sweepEvery is how often the server checks the session of every open socket once more
	sweepEvery = time.Minute
)

type Server struct {
	io         *socket.Server
	eventBus   events.EventBus
	tokenMaker *auth.TokenMaker
	workspaces workspace.Repository
	monitors   monitor.MonitorRepository
	logger     *zap.SugaredLogger
	// onConnection runs first in the connection handler, with the ID of the socket that connected.
	// Only tests set it: by then the library knows the socket, so a test waits for it before it
	// sends anything (see connectAs in the tests), and a test can slow the handler.
	onConnection func(id string)

	// monitorWorkspaces remembers the workspace of each monitor that has had a heartbeat or a
	// join, so that a heartbeat costs no read: a monitor never changes workspace. A deleted
	// monitor is forgotten.
	monitorWorkspaces sync.Map // monitor ID -> workspace.ID

	// sockets are the open sockets, by user, for taking a user out of a workspace's rooms
	socketsMu sync.Mutex
	sockets   map[string]map[socket.SocketId]*userSocket

	// sweepEvery is how often the sweep checks the session of every open socket: SessionsEnded
	// travels over Redis pub/sub, which does not replay an event a replica missed while it was
	// disconnected, so a socket of a disabled user could keep its rooms. Tests shorten it. The
	// sweep starts with the first socket, and stopSweep ends it.
	sweepEvery time.Duration
	sweepOnce  sync.Once
	stopSweep  chan struct{}
	closeOnce  sync.Once
}

// userSocket is an open socket and the lock that keeps its membership check and its join together
// against it being taken out of a workspace.
type userSocket struct {
	client *socket.Socket
	userID string
	// sessionStart is when the session of the socket's token began, nil when the token says not
	sessionStart *time.Time
	// mu is held while a join checks the membership and enters the room, and while the socket
	// leaves a workspace's rooms. A removal commits before MemberLeft is published, so a join that
	// passed its check before the removal is finished, and then left, before the leave returns.
	mu sync.Mutex
}

type SocketData struct {
	UserId string
	// SessionStart is when the session of the token the socket connected with began (its
	// auth_time, else its iat), which the later checks of the socket's session judge it by
	SessionStart *time.Time
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
		sweepEvery: sweepEvery,
		stopSweep:  make(chan struct{}),
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

		data := SocketData{UserId: fmt.Sprint(claims.UserID), SessionStart: claims.SessionStart()}

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
		us := server.register(client, data)
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
			// Only the rooms a client joins
			prefix, id, ok := parseRoom(roomName)
			if !ok {
				logger.Debugf("leave_room: ignored %q", roomName)
				reply(ack, "error", "not allowed")
				return
			}
			logger.Debugf("leave_room: %s", roomName)
			us.mu.Lock()
			client.Leave(socket.Room(prefix + id))
			us.mu.Unlock()
			reply(ack, "ok", "")
		}))

		next(nil)
	})

	io.On("connection", safe(logger, "connection", func(clients ...interface{}) {
		var client *socket.Socket
		if len(clients) > 0 {
			client, _ = clients[0].(*socket.Socket)
		}
		if client == nil {
			logger.Warnf("[WS]connection: dropped, no socket in %d arguments", len(clients))
			return
		}
		if server.onConnection != nil {
			server.onConnection(string(client.Id()))
		}
		// The auth middleware sets the data, so a socket without it never authenticated
		data, ok := client.Data().(SocketData)
		if !ok {
			logger.Warn("[WS]connection: disconnecting a socket without user data")
			client.Disconnect(true)
			return
		}

		// The socket was registered in a middleware, before the library marked it connected, and
		// socket.io v2.4.11's Socket.Disconnect does nothing to a socket that is not connected yet:
		// a SessionsEnded published in between closed nothing. Now that it is connected, its
		// session is checked once more.
		if err := tokenMaker.CheckSession(context.Background(), data.UserId, data.SessionStart); errors.Is(err, auth.ErrInvalidToken) {
			logger.Debugf("[WS]connection: closing a socket of user %s, whose session ended", data.UserId)
			client.Disconnect(true)
			return
		} else if err != nil {
			// The join checks the session again, and so does the sweep
			logger.Warnw("[WS]connection: could not check the session of a socket", "user_id", data.UserId, "error", err)
		}

		logger.Debugf("[WS]connection: %s", data.UserId)
	}))

	subscriber := events.NewSubscriber(eventBus, "websocket")

	// A heartbeat goes to its workspace's room and to its monitor's room, and to no other
	events.Subscribe(subscriber, events.HeartbeatEvent, func(ctx context.Context, hb heartbeat.Model) {
		monitorRoom := monitorPrefix + hb.MonitorID
		server.emitToRoom(monitorRoom, monitorRoom+":heartbeat", &hb)

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
		server.emitToRoom(workspaceRoom, workspaceRoom+":heartbeat", &hb)
	})

	events.Subscribe(subscriber, events.MonitorDeleted, func(_ context.Context, monitorID string) {
		server.monitorWorkspaces.Delete(monitorID)
	})

	events.Subscribe(subscriber, workspace.MemberLeft, server.memberLeft)
	events.Subscribe(subscriber, auth.SessionsEnded, server.sessionsEnded)

	return server, nil
}

// emitToRoom sends an event to each socket in the room, one socket at a time. It is not
// io.To(room).Emit: that broadcast marks its packet as pre-encoded, and engine.io's websocket
// transport (zishang520/engine.io v2.5.0) writes only the first packet of a batch that starts with
// a pre-encoded one and drops the rest. A client that gets two broadcasts while the transport is
// still writing the one before them loses all but the first, so a burst of heartbeats reaches the
// web app with holes in it. A packet from Socket.Emit isn't pre-encoded, and the transport sends
// every one of a batch of those.
func (s *Server) emitToRoom(room, event string, payload any) {
	namespace := s.io.Sockets()
	for _, id := range namespace.Adapter().Sockets(types.NewSet(socket.Room(room))).Keys() {
		client, ok := namespace.Sockets().Load(id)
		if !ok {
			continue
		}
		if err := client.Emit(event, payload); err != nil {
			s.logger.Warnw("Event not sent to a socket", "event", event, "error", err)
		}
	}
}

func (s *Server) register(client *socket.Socket, data SocketData) *userSocket {
	s.sweepOnce.Do(func() { go s.sweepLoop(s.sweepEvery) })
	us := &userSocket{client: client, userID: data.UserId, sessionStart: data.SessionStart}
	s.socketsMu.Lock()
	defer s.socketsMu.Unlock()
	if s.sockets[us.userID] == nil {
		s.sockets[us.userID] = map[socket.SocketId]*userSocket{}
	}
	s.sockets[us.userID][client.Id()] = us
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

// parseRoom splits a room name a client may ask for into its prefix and its ID. The ID has to be a
// UUID, so that nothing else is looked up in the database, and it comes back in its canonical
// lower-case form, the one the room is named by.
func parseRoom(name string) (prefix, id string, ok bool) {
	if len(name) > maxRoomName {
		return "", "", false
	}
	for _, p := range []string{workspacePrefix, monitorPrefix} {
		raw, found := strings.CutPrefix(name, p)
		if !found {
			continue
		}
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return "", "", false
		}
		return p, parsed.String(), true
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

	// A socket outlives the check of its token. A socket whose session ended since joins nothing,
	// though its memberships stand: the account was disabled or deleted, or its sessions were
	// ended (a password reset), and the socket may have missed SessionsEnded.
	if err := s.tokenMaker.CheckSession(ctx, us.userID, us.sessionStart); err != nil {
		if !errors.Is(err, auth.ErrInvalidToken) {
			s.logger.Warnw("join_room refused: could not read the user's session", "user_id", us.userID, "error", err)
			return "could not read the session"
		}
		return "the session ended"
	}

	memberships, err := s.workspaces.ListMemberships(ctx, us.userID)
	if err != nil {
		s.logger.Warnw("join_room refused: could not read the user's memberships", "user_id", us.userID, "error", err)
		return "could not read the memberships"
	}
	if !slices.ContainsFunc(memberships, func(m workspace.Membership) bool { return m.Workspace.ID == target }) {
		return "not a member of the workspace"
	}
	us.client.Join(socket.Room(prefix + id))
	return ""
}

// memberLeft takes every open socket of the user out of the workspace's room and the rooms of its
// monitors, so that they stop getting its updates with no reconnect.
func (s *Server) memberLeft(ctx context.Context, e workspace.MemberLeftEvent) {
	for _, us := range s.socketsOf(e.UserID) {
		s.leaveWorkspace(ctx, us, e.WorkspaceID)
	}
}

// sessionsEnded closes every open socket of the user: their sessions ended, and a socket is a
// session that no token check reaches again. The client may connect again, with the token of a new
// session if it has one.
func (s *Server) sessionsEnded(_ context.Context, e auth.SessionsEndedEvent) {
	for _, us := range s.socketsOf(e.UserID) {
		us.client.Disconnect(true)
	}
}

// sweepLoop runs the sweep every interval until the server closes
func (s *Server) sweepLoop(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopSweep:
			return
		case <-ticker.C:
			s.sweep(context.Background())
		}
	}
}

// sweep closes every open socket whose session ended, which SessionsEnded closes too, unless the
// replica missed it. It reads each user once. A user that can't be read is left for the next sweep.
func (s *Server) sweep(ctx context.Context) {
	s.socketsMu.Lock()
	byUser := make(map[string][]*userSocket, len(s.sockets))
	for userID, sockets := range s.sockets {
		for _, us := range sockets {
			byUser[userID] = append(byUser[userID], us)
		}
	}
	s.socketsMu.Unlock()

	for userID, sockets := range byUser {
		state, err := s.tokenMaker.SessionStateOf(ctx, userID)
		gone := errors.Is(err, auth.ErrInvalidToken)
		if err != nil && !gone {
			s.logger.Warnw("Sweep skipped the sockets of a user whose session could not be read", "user_id", userID, "error", err)
			continue
		}
		for _, us := range sockets {
			if gone || state.Ended(us.sessionStart) {
				s.logger.Debugf("Sweep closes a socket of user %s, whose session ended", userID)
				us.client.Disconnect(true)
			}
		}
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
	s.closeOnce.Do(func() { close(s.stopSweep) })
	s.io.Close(nil)
}
