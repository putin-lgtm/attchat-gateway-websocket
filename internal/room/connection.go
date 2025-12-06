package room

import (
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/rs/zerolog/log"
)

// Connection represents a WebSocket connection with metadata
type Connection struct {
	ID        string
	Conn      *websocket.Conn
	UserID    string
	BrandID   string
	Role      string
	Type      string // "cskh" or "customer"
	Device    string // thiết bị
	Tags      string // tags
	Timezone  string // múi giờ
	Channel   string // kênh
	Rooms     map[string]bool
	CreatedAt time.Time
	LastPing  time.Time
	mu        sync.RWMutex
	closed    bool
	send      chan []byte
}

// NewConnection creates a new connection wrapper
func NewConnection(id string, conn *websocket.Conn, userID, brandID, role, userType string) *Connection {
	c := &Connection{
		ID:        id,
		Conn:      conn,
		UserID:    userID,
		BrandID:   brandID,
		Role:      role,
		Type:      userType,
		Rooms:     make(map[string]bool),
		CreatedAt: time.Now(),
		LastPing:  time.Now(),
		send:      make(chan []byte, 256),
	}

	// Auto-join default rooms based on user type
	c.joinDefaultRooms()

	return c
}

// joinDefaultRooms joins rooms based on user type
func (c *Connection) joinDefaultRooms() {
	// Join user-specific room
	c.JoinRoom("user:" + c.UserID)

	// Join brand room
	if c.BrandID != "" {
		c.JoinRoom("brand:" + c.BrandID)
	}

	// CSKH joins additional rooms
	if c.Type == "cskh" {
		// Join folder/inbox rooms
		c.JoinRoom("folder:" + c.BrandID + ":all")
		c.JoinRoom("folder:" + c.BrandID + ":waiting")
		c.JoinRoom("folder:" + c.BrandID + ":active")
	}
}

// JoinRoom adds connection to a room
func (c *Connection) JoinRoom(roomID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Rooms[roomID] = true
	log.Debug().
		Str("conn_id", c.ID).
		Str("user_id", c.UserID).
		Str("room", roomID).
		Msg("Joined room")
}

// LeaveRoom removes connection from a room
func (c *Connection) LeaveRoom(roomID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Rooms, roomID)
	log.Debug().
		Str("conn_id", c.ID).
		Str("user_id", c.UserID).
		Str("room", roomID).
		Msg("Left room")
}

// IsInRoom checks if connection is in a room
func (c *Connection) IsInRoom(roomID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Rooms[roomID]
}

// GetRooms returns all rooms this connection is in
func (c *Connection) GetRooms() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rooms := make([]string, 0, len(c.Rooms))
	for room := range c.Rooms {
		rooms = append(rooms, room)
	}
	return rooms
}

// Send sends a message to this connection
func (c *Connection) Send(message []byte) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	select {
	case c.send <- message:
		return nil
	default:
		// Buffer full, connection is slow
		log.Warn().
			Str("conn_id", c.ID).
			Str("user_id", c.UserID).
			Msg("Send buffer full, dropping message")
		return nil
	}
}

// SendChannel returns the send channel for writing
func (c *Connection) SendChannel() <-chan []byte {
	return c.send
}

// UpdateLastPing updates the last ping time
func (c *Connection) UpdateLastPing() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastPing = time.Now()
}

// IsClosed checks if connection is closed
func (c *Connection) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// Close marks the connection as closed
func (c *Connection) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.send)
	}
}
