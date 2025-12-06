package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server  ServerConfig
	JWT     JWTConfig
	NATS    NATSConfig
	Metrics MetricsConfig
	WS      WebSocketConfig
}

type ServerConfig struct {
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type JWTConfig struct {
	SecretKey     string
	ValidateExp   bool
	AllowedIssuers []string
}

type NATSConfig struct {
	URL           string
	ClusterID     string
	ClientID      string
	ReconnectWait time.Duration
	MaxReconnects int
	Streams       []string
}

type MetricsConfig struct {
	Port    string
	Enabled bool
}

type WebSocketConfig struct {
	MaxConnections    int
	PingInterval      time.Duration
	PongTimeout       time.Duration
	WriteTimeout      time.Duration
	ReadBufferSize    int
	WriteBufferSize   int
	EnableCompression bool
	MaxMessageSize    int64
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	// Environment variable overrides
	viper.SetEnvPrefix("GATEWAY")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Set defaults
	setDefaults()

	// Read config file (optional)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// Config file not found, use defaults and env vars
	}

	cfg := &Config{
		Server: ServerConfig{
			Port:         viper.GetString("server.port"),
			ReadTimeout:  viper.GetDuration("server.read_timeout"),
			WriteTimeout: viper.GetDuration("server.write_timeout"),
		},
		JWT: JWTConfig{
			SecretKey:      viper.GetString("jwt.secret_key"),
			ValidateExp:    viper.GetBool("jwt.validate_exp"),
			AllowedIssuers: viper.GetStringSlice("jwt.allowed_issuers"),
		},
		NATS: NATSConfig{
			URL:           viper.GetString("nats.url"),
			ClusterID:     viper.GetString("nats.cluster_id"),
			ClientID:      viper.GetString("nats.client_id"),
			ReconnectWait: viper.GetDuration("nats.reconnect_wait"),
			MaxReconnects: viper.GetInt("nats.max_reconnects"),
			Streams:       viper.GetStringSlice("nats.streams"),
		},
		Metrics: MetricsConfig{
			Port:    viper.GetString("metrics.port"),
			Enabled: viper.GetBool("metrics.enabled"),
		},
		WS: WebSocketConfig{
			MaxConnections:    viper.GetInt("ws.max_connections"),
			PingInterval:      viper.GetDuration("ws.ping_interval"),
			PongTimeout:       viper.GetDuration("ws.pong_timeout"),
			WriteTimeout:      viper.GetDuration("ws.write_timeout"),
			ReadBufferSize:    viper.GetInt("ws.read_buffer_size"),
			WriteBufferSize:   viper.GetInt("ws.write_buffer_size"),
			EnableCompression: viper.GetBool("ws.enable_compression"),
			MaxMessageSize:    viper.GetInt64("ws.max_message_size"),
		},
	}

	return cfg, nil
}

func setDefaults() {
	// Server defaults
	viper.SetDefault("server.port", "8080")
	viper.SetDefault("server.read_timeout", "10s")
	viper.SetDefault("server.write_timeout", "10s")

	// JWT defaults
	viper.SetDefault("jwt.secret_key", "change-me-in-production")
	viper.SetDefault("jwt.validate_exp", true)
	viper.SetDefault("jwt.allowed_issuers", []string{"attchat"})

	// NATS defaults
	viper.SetDefault("nats.url", "nats://localhost:4222")
	viper.SetDefault("nats.cluster_id", "attchat")
	viper.SetDefault("nats.client_id", "gateway")
	viper.SetDefault("nats.reconnect_wait", "2s")
	viper.SetDefault("nats.max_reconnects", -1) // Unlimited
	viper.SetDefault("nats.streams", []string{"CHAT", "NOTIFY"})

	// Metrics defaults
	viper.SetDefault("metrics.port", "9090")
	viper.SetDefault("metrics.enabled", true)

	// WebSocket defaults
	viper.SetDefault("ws.max_connections", 10000)
	viper.SetDefault("ws.ping_interval", "30s")
	viper.SetDefault("ws.pong_timeout", "10s")
	viper.SetDefault("ws.write_timeout", "10s")
	viper.SetDefault("ws.read_buffer_size", 4096)
	viper.SetDefault("ws.write_buffer_size", 4096)
	viper.SetDefault("ws.enable_compression", false)
	viper.SetDefault("ws.max_message_size", 65536) // 64KB
}

