package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/attchat/attchat-gateway/internal/auth"
	"github.com/attchat/attchat-gateway/internal/config"
	"github.com/attchat/attchat-gateway/internal/metrics"
	"github.com/attchat/attchat-gateway/internal/nats"
	"github.com/attchat/attchat-gateway/internal/room"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	recovermw "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	cpuutil "github.com/shirou/gopsutil/v3/cpu"
	memutil "github.com/shirou/gopsutil/v3/mem"
	netutil "github.com/shirou/gopsutil/v3/net"
)

// Server represents the WebSocket server
type Server struct {
	app          *fiber.App
	cfg          *config.Config
	roomManager  *room.Manager
	jwtValidator *auth.JWTValidator
	nats         *nats.Consumer
}

type netSample struct {
	bytes uint64
	time  time.Time
}

var (
	netMu         sync.Mutex
	lastNetSample netSample
)

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
func New(cfg *config.Config, roomManager *room.Manager, natsConsumer *nats.Consumer) (*Server, error) {
	app := fiber.New(fiber.Config{
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	})

	// Middleware
	app.Use(recovermw.New())
	app.Use(logger.New(logger.Config{
		Format: "${time} | ${ip} | ${method} ${path} | ${status} | ${latency} | ${ua} | ${error}\n",
		CustomTags: map[string]logger.LogFunc{
			"status": func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				status := c.Response().StatusCode()
				var color string
				switch {
				case status == 101:
					color = "\033[32m" // Green for WebSocket upgrade
				case status >= 500:
					color = "\033[31m" // Red
				case status >= 400:
					color = "\033[33m" // Yellow
				case status >= 300:
					color = "\033[36m" // Cyan
				case status >= 100:
					color = "\033[0m" // Default
				default:
					color = "\033[0m"
				}
				return output.WriteString(color + fmt.Sprintf("%3d", status) + "\033[0m")
			},
		},
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
	}))

	validator, err := auth.NewJWTValidator(cfg.JWT)
	if err != nil {
		return nil, err
	}

	s := &Server{
		app:          app,
		cfg:          cfg,
		roomManager:  roomManager,
		jwtValidator: validator,
		nats:         natsConsumer,
	}

	s.setupRoutes()

	return s, nil
}

// setupRoutes configures all routes
func (s *Server) setupRoutes() {
	// Root endpoint trả về thông tin health
	s.app.Get("/", func(c *fiber.Ctx) error {
		streams, consumers := s.jetStreamCounts()
		sys := systemMetrics()
		return c.JSON(fiber.Map{
			"architecture": "MVC Enterprise",
			"jetstream":    "ok",
			"jetstream_info": fiber.Map{
				"consumers": consumers,
				"streams":   streams,
			},
			"message": "ATTChat Gateway WebSocket is running",
			"nats":    "ok",
			"status":  "healthy",
			"version": "2.0",
			"stats":   s.roomManager.GetStats(),
			"system":  sys,
		})
	})
	// Health check
	s.app.Get("/health", func(c *fiber.Ctx) error {
		streams, consumers := s.jetStreamCounts()
		sys := systemMetrics()
		return c.JSON(fiber.Map{
			"architecture": "MVC Enterprise",
			"jetstream":    "ok", // Giả sử luôn ok, có thể kiểm tra thực tế nếu cần
			"jetstream_info": fiber.Map{
				"consumers": consumers, // Có thể lấy từ metrics nếu có
				"streams":   streams,   // hoặc tên stream thực tế
			},
			"message": "ATTChat Gateway WebSocket is running",
			"nats":    "ok", // Giả sử luôn ok, có thể kiểm tra thực tế nếu cần
			"status":  "healthy",
			"version": "2.0",
			"stats":   s.roomManager.GetStats(),
			"system":  sys,
		})
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
		log.Warn().
			Err(err).
			Str("token_prefix", prefixToken(token)).
			Str("iss", claimsIssuer(token)).
			Strs("allowed_issuers", s.cfg.JWT.AllowedIssuers).
			Msg("JWT validation failed")
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
	if claims.UserID != 0 {
		userID = fmt.Sprintf("%d", claims.UserID)
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

func (s *Server) jetStreamCounts() (streams int64, consumers int64) {
	if s.nats == nil {
		return 0, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	streams, consumers, err := s.nats.AccountStats(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get JetStream account info")
		return 0, 0
	}
	return streams, consumers
}

type systemInfo struct {
	CPUPercent string `json:"cpu_used_percent"`
	RAMPercent string `json:"ram_used_percent"`
	RAMUsedMB  string `json:"ram_used_mb"`
	NetMbps    string `json:"net_mbps"`
}

func systemMetrics() systemInfo {
	cpuP := cpuPercent()
	memP, memUsed := memStats()
	netMbps := netThroughputMbps()
	return systemInfo{
		CPUPercent: fmtPercent(cpuP),
		RAMPercent: fmtPercent(memP),
		RAMUsedMB:  fmtMB(memUsed),
		NetMbps:    fmtMbps(netMbps),
	}
}

func fmtPercent(v float64) string {
	if v < 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.2f%%", v)
}

func fmtMB(v float64) string {
	if v < 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.2f MB", v)
}

func fmtMbps(v float64) string {
	if v < 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.2f Mbps", v)
}

func cpuPercent() float64 {
	perc, err := cpuutil.Percent(0, false)
	if err != nil || len(perc) == 0 {
		return -1
	}
	return perc[0]
}

func memStats() (percent float64, usedMB float64) {
	vm, err := memutil.VirtualMemory()
	if err != nil {
		return -1, -1
	}
	return vm.UsedPercent, float64(vm.Used) / (1024 * 1024)
}

func netThroughputMbps() float64 {
	counters, err := netutil.IOCounters(true)
	if err != nil || len(counters) == 0 {
		return -1
	}
	var total uint64
	for _, c := range counters {
		total += c.BytesRecv + c.BytesSent
	}
	now := time.Now()

	netMu.Lock()
	defer netMu.Unlock()

	if lastNetSample.time.IsZero() {
		lastNetSample = netSample{bytes: total, time: now}
		return 0
	}
	deltaBytes := total - lastNetSample.bytes
	elapsed := now.Sub(lastNetSample.time).Seconds()
	lastNetSample = netSample{bytes: total, time: now}
	if elapsed <= 0 {
		return 0
	}
	mbps := (float64(deltaBytes) * 8) / (elapsed * 1_000_000) // bytes -> bits -> megabits
	return mbps
}

func prefixToken(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:12] + "..."
}

// claimsIssuer extracts issuer without verifying signature (best effort for logging)
func claimsIssuer(tokenString string) string {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := jwt.MapClaims{}
	_, _ = parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) { return nil, nil })
	if iss, ok := claims["iss"].(string); ok {
		return iss
	}
	return ""
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
		// Forward other message types to NATS for backend consumers
		event := nats.Event{
			Type:          msg.Type,
			Room:          msg.Room,
			UserID:        conn.UserID,
			BrandID:       conn.BrandID,
			Payload:       msg.Payload,
			Timestamp:     time.Now(),
			ExcludeConnID: conn.ID,
		}

		data, err := json.Marshal(event)
		if err != nil {
			log.Error().Err(err).Str("conn_id", conn.ID).Msg("Failed to marshal event for NATS")
			return
		}

		if s.nats == nil {
			log.Warn().Str("conn_id", conn.ID).Msg("NATS publisher not configured")
			return
		}

		subject := "CHAT.events"
		if err := s.nats.Publish(subject, data); err != nil {
			log.Error().Err(err).Str("conn_id", conn.ID).Str("subject", subject).Msg("Failed to publish to NATS")
			return
		}

		log.Debug().
			Str("conn_id", conn.ID).
			Str("subject", subject).
			Str("type", msg.Type).
			Msg("Forwarded message to NATS")
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
