package websocket

import (
	"context"
	"fmt"
	"net/http"
	"peekaping/src/config"
	"peekaping/src/modules/auth"
	"peekaping/src/modules/events"
	"peekaping/src/modules/heartbeat"

	"github.com/zishang520/socket.io/v2/socket"
	"go.uber.org/zap"
)

type Server struct {
	io         *socket.Server
	eventBus   *events.EventBus
	tokenMaker *auth.TokenMaker
}

type SocketData struct {
	UserId string
}

func NewServer(
	cfg *config.Config,
	eventBus *events.EventBus,
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
		access_token, ok := s.Request().Query().Get("token")
		if !ok {
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

	io.On("connection", func(clients ...interface{}) {
		client := clients[0].(*socket.Socket)
		userId := client.Data().(SocketData).UserId

		logger.Debugf("[WS]connection: %s", userId)

		client.On("join_room", func(args ...interface{}) {
			roomName := args[0].(string)
			logger.Debugf("join_room: %s", roomName)

			// TODO: validate if user allowed to join room
			client.Join(socket.Room(roomName))
			// ack([]interface{}{map[string]string{"status": "ok"}}, nil)
		})

		client.On("leave_room", func(args ...interface{}) {
			roomName := args[0].(string)
			logger.Debugf("leave_room: %s", roomName)
			// ack := args[1].(func([]interface{}, error))
			client.Leave(socket.Room(roomName))
			// ack([]interface{}{map[string]string{"status": "ok"}}, nil)
		})
	})

	// Listen for heartbeat events and broadcast to room
	eventBus.Subscribe(events.HeartbeatEvent, func(event events.Event) {
		heartbeat := event.Payload.(*heartbeat.Model)
		roomName := "monitor:" + heartbeat.MonitorID
		server.io.To(socket.Room(roomName)).Emit(roomName+":heartbeat", event.Payload)
		server.io.To(socket.Room("monitor:all")).Emit("monitor:all:heartbeat", event.Payload)
	})

	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.io.ServeHandler(nil).ServeHTTP(w, r)
}

func (s *Server) Close() {
	s.io.Close(nil)
}
