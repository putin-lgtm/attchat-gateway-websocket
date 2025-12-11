package room

import (
	"sync"
	"sync/atomic"

	"github.com/attchat/attchat-gateway/internal/metrics"
	"github.com/rs/zerolog/log"
)

// Manager manages all connections and rooms
type Manager struct {
	// Stats (đặt lên đầu để đảm bảo alignment)
	totalConnections int64
	totalRooms       int64

	// All connections indexed by ID
	connections sync.Map // map[string]*Connection

	// Room to connections mapping
	rooms sync.Map // map[string]map[string]*Connection

	// User to connections mapping (for multi-tab support)
	userConnections sync.Map // map[string]map[string]*Connection
}

// NewManager creates a new room manager
func NewManager() *Manager {
	return &Manager{}
}

// AddConnection adds a new connection
func (m *Manager) AddConnection(conn *Connection) {
	// Store connection
	m.connections.Store(conn.ID, conn)

	// Add to user's connections (multi-tab support)
	userConns, _ := m.userConnections.LoadOrStore(conn.UserID, &sync.Map{})
	userConns.(*sync.Map).Store(conn.ID, conn)

	// Add to all rooms
	for room := range conn.Rooms {
		m.addToRoom(room, conn)
	}

	// Update stats
	atomic.AddInt64(&m.totalConnections, 1)
	metrics.ConnectionsTotal.Inc()
	metrics.ConnectionsCurrent.Inc()

	log.Info().
		Str("conn_id", conn.ID).
		Str("user_id", conn.UserID).
		Str("brand_id", conn.BrandID).
		Str("type", conn.Type).
		Int64("total", atomic.LoadInt64(&m.totalConnections)).
		Msg("Connection added")
}

// RemoveConnection removes a connection
func (m *Manager) RemoveConnection(connID string) {
	connInterface, ok := m.connections.Load(connID)
	if !ok {
		return
	}

	conn := connInterface.(*Connection)

	// Remove from all rooms
	for room := range conn.Rooms {
		m.removeFromRoom(room, connID)
	}

	// Remove from user's connections
	if userConns, ok := m.userConnections.Load(conn.UserID); ok {
		userConns.(*sync.Map).Delete(connID)
	}

	// Remove connection
	m.connections.Delete(connID)

	// Mark as closed
	conn.Close()

	// Update stats
	metrics.ConnectionsCurrent.Dec()

	log.Info().
		Str("conn_id", conn.ID).
		Str("user_id", conn.UserID).
		Msg("Connection removed")
}

// GetConnection gets a connection by ID
func (m *Manager) GetConnection(connID string) (*Connection, bool) {
	if conn, ok := m.connections.Load(connID); ok {
		return conn.(*Connection), true
	}
	return nil, false
}

// GetUserConnections gets all connections for a user
func (m *Manager) GetUserConnections(userID string) []*Connection {
	var connections []*Connection

	if userConns, ok := m.userConnections.Load(userID); ok {
		userConns.(*sync.Map).Range(func(_, value interface{}) bool {
			connections = append(connections, value.(*Connection))
			return true
		})
	}

	return connections
}

// JoinRoom adds a connection to a room
func (m *Manager) JoinRoom(connID, roomID string) {
	conn, ok := m.GetConnection(connID)
	if !ok {
		return
	}

	conn.JoinRoom(roomID)
	m.addToRoom(roomID, conn)
}

// LeaveRoom removes a connection from a room
func (m *Manager) LeaveRoom(connID, roomID string) {
	conn, ok := m.GetConnection(connID)
	if !ok {
		return
	}

	conn.LeaveRoom(roomID)
	m.removeFromRoom(roomID, connID)
}

// addToRoom adds a connection to a room
func (m *Manager) addToRoom(roomID string, conn *Connection) {
	roomConns, loaded := m.rooms.LoadOrStore(roomID, &sync.Map{})
	roomConns.(*sync.Map).Store(conn.ID, conn)

	if !loaded {
		atomic.AddInt64(&m.totalRooms, 1)
		metrics.RoomsTotal.Inc()
	}
}

// removeFromRoom removes a connection from a room
func (m *Manager) removeFromRoom(roomID, connID string) {
	if roomConns, ok := m.rooms.Load(roomID); ok {
		roomConns.(*sync.Map).Delete(connID)

		// Check if room is empty
		isEmpty := true
		roomConns.(*sync.Map).Range(func(_, _ interface{}) bool {
			isEmpty = false
			return false
		})

		if isEmpty {
			m.rooms.Delete(roomID)
			atomic.AddInt64(&m.totalRooms, -1)
			metrics.RoomsTotal.Dec()
		}
	}
}

// BroadcastToRoom sends a message to all connections in a room
func (m *Manager) BroadcastToRoom(roomID string, message []byte, excludeConnID string) int {
	roomConns, ok := m.rooms.Load(roomID)
	if !ok {
		return 0
	}

	count := 0
	roomConns.(*sync.Map).Range(func(connID, connInterface interface{}) bool {
		if connID.(string) == excludeConnID {
			return true
		}

		conn := connInterface.(*Connection)
		if err := conn.Send(message); err == nil {
			count++
		}
		return true
	})

	metrics.MessagesSent.Add(float64(count))
	return count
}

// BroadcastToUser sends a message to all connections of a user
func (m *Manager) BroadcastToUser(userID string, message []byte, excludeConnID string) int {
	connections := m.GetUserConnections(userID)
	count := 0

	for _, conn := range connections {
		if conn.ID == excludeConnID {
			continue
		}
		if err := conn.Send(message); err == nil {
			count++
		}
	}

	return count
}

// GetStats returns current statistics
func (m *Manager) GetStats() map[string]int64 {
	// Count current live connections (sync.Map length)
	var current int64
	m.connections.Range(func(_, _ interface{}) bool {
		current++
		return true
	})

	return map[string]int64{
		"total_connections":   atomic.LoadInt64(&m.totalConnections),
		"current_connections": current,
		"total_rooms":         atomic.LoadInt64(&m.totalRooms),
	}
}

// GetRoomConnections returns all connections in a room
func (m *Manager) GetRoomConnections(roomID string) []*Connection {
	var connections []*Connection

	if roomConns, ok := m.rooms.Load(roomID); ok {
		roomConns.(*sync.Map).Range(func(_, connInterface interface{}) bool {
			connections = append(connections, connInterface.(*Connection))
			return true
		})
	}

	return connections
}
