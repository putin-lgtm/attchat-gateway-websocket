# ATTChat Gateway

**High-performance stateless WebSocket realtime gateway** – chịu > 600k concurrent connections.  
Chỉ làm đúng 1 việc: vận chuyển tin nhắn realtime cực nhanh, cực chính xác.

![Go](https://img.shields.io/badge/Go-1.23-blue?logo=go) ![NATS](https://img.shields.io/badge/NATS_JetStream-2.10-success) ![600k+](https://img.shields.io/badge/600k%2B%20connections-green)

## 🏗️ Architecture

```
                     Clients
                        │
                        │ WebSocket
                        ▼
┌─────────────────────────────────────────────────────────┐
│                   ATTCHAT-GATEWAY                        │
│                                                          │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────┐  │
│   │ JWT Auth     │    │ Room Manager │    │ Metrics  │  │
│   │ < 50ms       │    │ Multi-room   │    │ Prometheus│  │
│   └──────────────┘    └──────────────┘    └──────────┘  │
│                              │                           │
│                              ▼                           │
│                    ┌──────────────────┐                  │
│                    │ NATS Consumer    │                  │
│                    │ Pull + Broadcast │                  │
│                    └──────────────────┘                  │
│                                                          │
└─────────────────────────────────────────────────────────┘
                        │
                        │ NATS JetStream
                        ▼
              ┌───────────────────┐
              │  Chat Service     │
              │  (Business Logic) │
              └───────────────────┘
```

## 🚀 Quick Start

### Local Development

```bash
# 1. Start shared infrastructure (PostgreSQL, Redis, NATS)
cd ../attchat-infra/docker
docker-compose up -d

# 2. Run Gateway
cd ../attchat-gateway
go run main.go

# 3. Check health
curl http://localhost:8086/health

# 4. View metrics
curl http://localhost:9090/metrics
```

### Build Binary

```bash
# Install dependencies
go mod tidy

# Build binary
go build -o gateway .
./gateway
```

## 📡 WebSocket API

### Connect

```javascript
const ws = new WebSocket('ws://localhost:8086/ws?token=YOUR_JWT_TOKEN');

ws.onopen = () => {
  console.log('Connected!');
};

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Received:', data);
};
```

### Message Types

#### Client → Server

```json
// Ping
{"type": "ping"}

// Join room
{"type": "join", "room": "chat:123"}

// Leave room
{"type": "leave", "room": "chat:123"}

// Typing indicator
{"type": "typing", "room": "chat:123"}
```

#### Server → Client

```json
// Connected
{"type": "connected", "payload": {"conn_id": "xxx"}}

// Pong
{"type": "pong", "timestamp": "2024-01-01T00:00:00Z"}

// Room joined
{"type": "joined", "room": "chat:123"}

// Chat message (from NATS)
{"type": "message", "room": "chat:123", "payload": {...}}

// Typing indicator
{"type": "typing", "room": "chat:123", "payload": {"user_id": "456"}}
```

## 🔐 JWT Token Structure

```json
{
  "sub": "user-id",
  "user_id": "12345",
  "brand_id": "brand-1",
  "role": "cskh",
  "type": "cskh",
  "rooms": ["folder:brand-1:all"],
  "iss": "attchat",
  "exp": 1234567890
}
```

## 📊 Metrics

Available at `:9090/metrics`

| Metric | Description |
|--------|-------------|
| `gateway_connections_current` | Current active connections |
| `gateway_connections_total` | Total connections since start |
| `gateway_messages_received_total` | Messages from clients |
| `gateway_messages_sent_total` | Messages to clients |
| `gateway_messages_from_nats_total` | Messages from NATS |
| `gateway_message_latency_seconds` | Processing latency |
| `gateway_rooms_total` | Active rooms count |
| `gateway_auth_success_total` | Successful authentications |
| `gateway_auth_failure_total` | Failed authentications |

## ⚙️ Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GATEWAY_SERVER_PORT` | 8086 | WebSocket server port |
| `GATEWAY_NATS_URL` | nats://localhost:4222 | NATS server URL |
| `GATEWAY_JWT_SECRET_KEY` | (required) | JWT secret key |
| `GATEWAY_METRICS_PORT` | 9090 | Prometheus metrics port |
| `GATEWAY_WS_MAX_CONNECTIONS` | 10000 | Max connections per node |
| `GATEWAY_WS_PING_INTERVAL` | 30s | Ping interval |
| `LOG_LEVEL` | info | Log level (debug, info, warn, error) |

### config.yaml

```yaml
server:
  port: "8086"
  read_timeout: "10s"
  write_timeout: "10s"

jwt:
  secret_key: "your-secret-key"
  validate_exp: true
  allowed_issuers:
    - "attchat"

nats:
  url: "nats://localhost:4222"
  streams:
    - "CHAT"
    - "NOTIFY"

ws:
  max_connections: 10000
  ping_interval: "30s"
```

## 🏃 Room Types

| Room Pattern | Description | Example |
|--------------|-------------|---------|
| `user:{id}` | User-specific events | `user:12345` |
| `brand:{id}` | Brand-wide events | `brand:abc` |
| `chat:{id}` | Specific chat room | `chat:chat-123` |
| `folder:{brand}:{type}` | Inbox folders | `folder:abc:waiting` |

## 📦 Project Structure

```
attchat-gateway/
├── main.go                 # Entry point
├── config.yaml             # Configuration
├── Dockerfile              # Container build
└── internal/
    ├── auth/
    │   └── jwt.go          # JWT validation
    ├── config/
    │   └── config.go       # Configuration loader
    ├── metrics/
    │   └── metrics.go      # Prometheus metrics
    ├── nats/
    │   └── consumer.go     # NATS JetStream consumer
    ├── room/
    │   ├── connection.go   # WebSocket connection
    │   └── manager.go      # Room management
    └── server/
        └── server.go       # HTTP/WebSocket server
```

## 🎯 Performance Targets

| Metric | Target |
|--------|--------|
| Connections per node | 10,000+ |
| JWT validation | < 50ms |
| Message latency (NATS → Client) | < 1ms |
| Memory per connection | ~50KB |
| Reconnect time | < 3s |

## Tech Stack

- **Go 1.23** - Language
- **Fiber** - HTTP framework
- **gorilla/websocket** - WebSocket
- **NATS JetStream** - Message queue
- **golang-jwt/jwt** - JWT validation
- **zerolog** - Structured logging
- **viper** - Configuration
- **prometheus/client_golang** - Metrics
