package nats

import (
	"context"
	"encoding/json"
	"time"

	"github.com/attchat/attchat-gateway/internal/config"
	"github.com/attchat/attchat-gateway/internal/metrics"
	"github.com/attchat/attchat-gateway/internal/room"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// Event represents a message event from NATS
type Event struct {
	Type      string          `json:"type"`
	Room      string          `json:"room"`
	UserID    string          `json:"user_id,omitempty"`
	BrandID   string          `json:"brand_id,omitempty"`
	ChatID    string          `json:"chat_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp time.Time       `json:"timestamp"`
	ExcludeConnID string      `json:"exclude_conn_id,omitempty"`
}

// Consumer consumes messages from NATS JetStream
type Consumer struct {
	nc          *nats.Conn
	js          jetstream.JetStream
	cfg         config.NATSConfig
	roomManager *room.Manager
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewConsumer creates a new NATS consumer
func NewConsumer(cfg config.NATSConfig, roomManager *room.Manager) (*Consumer, error) {
	// Connect to NATS
	opts := []nats.Option{
		nats.Name(cfg.ClientID),
		nats.ReconnectWait(cfg.ReconnectWait),
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			log.Warn().Err(err).Msg("NATS disconnected")
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Info().Str("url", nc.ConnectedUrl()).Msg("NATS reconnected")
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			log.Info().Msg("NATS connection closed")
		}),
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, err
	}

	// Create JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Consumer{
		nc:          nc,
		js:          js,
		cfg:         cfg,
		roomManager: roomManager,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Start starts consuming messages from all configured streams
func (c *Consumer) Start() {
	for _, streamName := range c.cfg.Streams {
		go c.consumeStream(streamName)
	}
}

// consumeStream consumes messages from a specific stream
func (c *Consumer) consumeStream(streamName string) {
	log.Info().Str("stream", streamName).Msg("Starting stream consumer")

	// Create or get consumer
	consumerName := "gateway-" + c.cfg.ClientID

	consumer, err := c.js.CreateOrUpdateConsumer(c.ctx, streamName, jetstream.ConsumerConfig{
		Name:          consumerName,
		Durable:       consumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
		FilterSubject: streamName + ".>",
	})
	if err != nil {
		log.Error().Err(err).Str("stream", streamName).Msg("Failed to create consumer")
		return
	}

	// Consume messages
	msgs, err := consumer.Messages()
	if err != nil {
		log.Error().Err(err).Str("stream", streamName).Msg("Failed to get messages")
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			msgs.Stop()
			return
		default:
			msg, err := msgs.Next()
			if err != nil {
				if err == context.Canceled {
					return
				}
				log.Error().Err(err).Msg("Error getting next message")
				continue
			}

			c.handleMessage(msg)
		}
	}
}

// handleMessage processes a message from NATS
func (c *Consumer) handleMessage(msg jetstream.Msg) {
	start := time.Now()

	var event Event
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal event")
		msg.Ack()
		return
	}

	// Update metrics
	metrics.MessagesFromNATS.Inc()

	// Route message to appropriate room(s)
	c.routeEvent(&event)

	// Ack message
	msg.Ack()

	// Record latency
	metrics.MessageLatency.Observe(time.Since(start).Seconds())

	log.Debug().
		Str("type", event.Type).
		Str("room", event.Room).
		Dur("latency", time.Since(start)).
		Msg("Event processed")
}

// routeEvent routes an event to the appropriate connections
func (c *Consumer) routeEvent(event *Event) {
	// Serialize event for sending
	data, err := json.Marshal(event)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal event")
		return
	}

	switch {
	case event.Room != "":
		// Broadcast to specific room
		count := c.roomManager.BroadcastToRoom(event.Room, data, event.ExcludeConnID)
		log.Debug().
			Str("room", event.Room).
			Int("recipients", count).
			Msg("Broadcasted to room")

	case event.UserID != "":
		// Send to specific user (all their connections)
		count := c.roomManager.BroadcastToUser(event.UserID, data, event.ExcludeConnID)
		log.Debug().
			Str("user_id", event.UserID).
			Int("recipients", count).
			Msg("Sent to user")

	default:
		log.Warn().Msg("Event has no routing target")
	}
}

// Publish publishes a message to NATS (for forwarding client messages)
func (c *Consumer) Publish(subject string, data []byte) error {
	_, err := c.js.Publish(c.ctx, subject, data)
	return err
}

// Close closes the NATS connection
func (c *Consumer) Close() {
	c.cancel()
	c.nc.Close()
}

