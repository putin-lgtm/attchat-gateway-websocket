package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/attchat/attchat-gateway/internal/auth"
	"github.com/attchat/attchat-gateway/internal/config"
	"github.com/attchat/attchat-gateway/internal/metrics"
	"github.com/attchat/attchat-gateway/internal/room"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	recovermw "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Server represents the WebSocket server
type Server struct {
	app          *fiber.App
	cfg          *config.Config
	roomManager  *room.Manager
	jwtValidator *auth.JWTValidator
}

// ClientMessage represents a message from client
type ClientMessage struct {
	Type    string          `json:"type"`
	Room    string          `json:"room,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ServerMessage represents a message to client
type ServerMessage struct {
	Type      string          `json:"type"`
	Room      string          `json:"room,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// New creates a new server
func New(cfg *config.Config, roomManager *room.Manager) *Server {
	app := fiber.New(fiber.Config{
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	})

	// Middleware
	app.Use(recovermw.New())
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
	}))

	s := &Server{
		app:          app,
		cfg:          cfg,
		roomManager:  roomManager,
		jwtValidator: auth.NewJWTValidator(cfg.JWT),
	}

	s.setupRoutes()

	return s
}

// setupRoutes configures all routes
func (s *Server) setupRoutes() {
	// Health check
	s.app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Ready check
	s.app.Get("/ready", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ready"})
	})

	// Stats endpoint
	s.app.Get("/stats", func(c *fiber.Ctx) error {
		return c.JSON(s.roomManager.GetStats())
	})

	// WebSocket upgrade middleware
	s.app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	// WebSocket endpoint
	s.app.Get("/ws", websocket.New(s.handleWebSocket, websocket.Config{
		ReadBufferSize:  s.cfg.WS.ReadBufferSize,
		WriteBufferSize: s.cfg.WS.WriteBufferSize,
	}))
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(c *websocket.Conn) {
	// Parse all connection params
	token := c.Query("token")
	if token == "" {
		token = c.Headers("Authorization")
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
	}

	userType := c.Query("user_type")
	role := c.Query("role")
	device := c.Query("device")
	tags := c.Query("tags")
	tz := c.Query("tz")
	brandID := c.Query("brand_id")
	userID := c.Query("user_id")
	roomID := c.Query("room_id")
	channel := c.Query("channel")

	// Validate JWT
	claims, err := s.jwtValidator.Validate(token)
	if err != nil {
		log.Warn().Err(err).Msg("JWT validation failed")
		metrics.AuthFailure.Inc()
		c.WriteJSON(ServerMessage{
			Type:      "error",
			Payload:   json.RawMessage(`{"code": "AUTH_FAILED", "message": "Invalid token"}`),
			Timestamp: time.Now(),
		})
		c.Close()
		return
	}

	metrics.AuthSuccess.Inc()

	// Ưu tiên lấy từ JWT claims, nếu không có thì lấy từ query
	if claims.UserID != "" {
		userID = claims.UserID
	}
	if claims.BrandID != "" {
		brandID = claims.BrandID
	}
	if claims.Role != "" {
		role = claims.Role
	}
	if claims.Type != "" {
		userType = claims.Type
	}

	// Create connection với metadata mở rộng
	connID := uuid.New().String()
	conn := room.NewConnection(connID, c, userID, brandID, role, userType)
	// Lưu thêm các metadata mới
	conn.Device = device
	conn.Tags = tags
	conn.Timezone = tz
	conn.Channel = channel

	// Add to manager
	s.roomManager.AddConnection(conn)
	defer s.roomManager.RemoveConnection(connID)

	// Join room từ query hoặc JWT
	if roomID != "" {
		s.roomManager.JoinRoom(connID, roomID)
	}
	for _, r := range claims.Rooms {
		s.roomManager.JoinRoom(connID, r)
	}

	// Send welcome message kèm metadata
	welcome := map[string]interface{}{
		"conn_id":   connID,
		"user_id":   userID,
		"brand_id":  brandID,
		"role":      role,
		"user_type": userType,
		"device":    device,
		"tags":      tags,
		"tz":        tz,
		"channel":   channel,
		"room_id":   roomID,
	}
	welcomeBytes, _ := json.Marshal(welcome)
	c.WriteJSON(ServerMessage{
		Type:      "connected",
		Payload:   welcomeBytes,
		Timestamp: time.Now(),
	})

	// Start writer goroutine
	go s.writeLoop(conn)

	// Read loop
	s.readLoop(conn)
}

// readLoop reads messages from client
func (s *Server) readLoop(conn *room.Connection) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("conn_id", conn.ID).Msg("Panic in read loop")
		}
	}()

	for {
		_, msg, err := conn.Conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Debug().Str("conn_id", conn.ID).Msg("Connection closed normally")
			} else {
				log.Debug().Err(err).Str("conn_id", conn.ID).Msg("Read error")
			}
			break
		}

		metrics.MessagesReceived.Inc()
		conn.UpdateLastPing()

		// Parse message
		var clientMsg ClientMessage
		if err := json.Unmarshal(msg, &clientMsg); err != nil {
			log.Warn().Err(err).Str("conn_id", conn.ID).Msg("Invalid message format")
			continue
		}

		s.handleClientMessage(conn, &clientMsg)
	}
}

// writeLoop writes messages to client
func (s *Server) writeLoop(conn *room.Connection) {
	pingTicker := time.NewTicker(s.cfg.WS.PingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case msg, ok := <-conn.SendChannel():
			if !ok {
				// Channel closed
				return
			}

			conn.Conn.SetWriteDeadline(time.Now().Add(s.cfg.WS.WriteTimeout))
			if err := conn.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Debug().Err(err).Str("conn_id", conn.ID).Msg("Write error")
				return
			}

		case <-pingTicker.C:
			conn.Conn.SetWriteDeadline(time.Now().Add(s.cfg.WS.WriteTimeout))
			if err := conn.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Debug().Err(err).Str("conn_id", conn.ID).Msg("Ping error")
				return
			}
		}
	}
}

// handleClientMessage processes a message from client
func (s *Server) handleClientMessage(conn *room.Connection, msg *ClientMessage) {
	switch msg.Type {
	case "ping":
		// Respond with pong
		conn.Send([]byte(`{"type":"pong","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`))

	case "join":
		// Join a room
		if msg.Room != "" {
			s.roomManager.JoinRoom(conn.ID, msg.Room)
			conn.Send([]byte(`{"type":"joined","room":"` + msg.Room + `"}`))
		}

	case "leave":
		// Leave a room
		if msg.Room != "" {
			s.roomManager.LeaveRoom(conn.ID, msg.Room)
			conn.Send([]byte(`{"type":"left","room":"` + msg.Room + `"}`))
		}

	case "typing":
		// Broadcast typing indicator to room
		if msg.Room != "" {
			typingMsg := ServerMessage{
				Type:      "typing",
				Room:      msg.Room,
				Payload:   json.RawMessage(`{"user_id":"` + conn.UserID + `","type":"` + conn.Type + `"}`),
				Timestamp: time.Now(),
			}
			data, _ := json.Marshal(typingMsg)
			s.roomManager.BroadcastToRoom(msg.Room, data, conn.ID)
		}

	default:
		log.Debug().
			Str("conn_id", conn.ID).
			Str("type", msg.Type).
			Msg("Unknown message type")
	}
}

// Start starts the server
func (s *Server) Start() {
	log.Info().Str("port", s.cfg.Server.Port).Msg("Starting WebSocket server")
	if err := s.app.Listen(":" + s.cfg.Server.Port); err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.app.ShutdownWithContext(ctx)
}
