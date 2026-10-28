package websocket

import (
	"context"
	"fmt"
	"net/http"
	"peekaping/internal/config"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"runtime/debug"

	"github.com/zishang520/socket.io/v2/socket"
	"go.uber.org/zap"
)

type Server struct {
	io         *socket.Server
	eventBus   events.EventBus
	tokenMaker *auth.TokenMaker
	// onConnection runs first in the connection handler. Only tests set it, to slow the handler.
	onConnection func()
}

type SocketData struct {
	UserId string
}

func NewServer(
	cfg *config.Config,
	eventBus events.EventBus,
	tokenMaker *auth.TokenMaker,
	logger *zap.SugaredLogger,
) (*Server, error) {
	opts := socket.DefaultServerOptions()
	io := socket.NewServer(nil, opts)

	server := &Server{
		io:         io,
		eventBus:   eventBus,
		tokenMaker: tokenMaker,
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
		client.On("join_room", safe(logger, "join_room", func(args ...interface{}) {
			roomName, ok := firstString(args)
			if !ok {
				logger.Debugf("join_room: dropped, no room name in %d arguments", len(args))
				return
			}
			logger.Debugf("join_room: %s", roomName)

			// Any signed-in user may join any room: the app is single-tenant, so every
			// user sees every monitor. Per-organization rooms will need a check here.
			client.Join(socket.Room(roomName))
			// ack([]interface{}{map[string]string{"status": "ok"}}, nil)
		}))

		client.On("leave_room", safe(logger, "leave_room", func(args ...interface{}) {
			roomName, ok := firstString(args)
			if !ok {
				logger.Debugf("leave_room: dropped, no room name in %d arguments", len(args))
				return
			}
			logger.Debugf("leave_room: %s", roomName)
			// ack := args[1].(func([]interface{}, error))
			client.Leave(socket.Room(roomName))
			// ack([]interface{}{map[string]string{"status": "ok"}}, nil)
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

	// Listen for heartbeat events and broadcast to room
	events.Subscribe(events.NewSubscriber(eventBus, "websocket"), events.HeartbeatEvent, func(_ context.Context, hb heartbeat.Model) {
		roomName := "monitor:" + hb.MonitorID
		server.io.To(socket.Room(roomName)).Emit(roomName+":heartbeat", &hb)
		server.io.To(socket.Room("monitor:all")).Emit("monitor:all:heartbeat", &hb)
	})

	return server, nil
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
