package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
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
	PublicKeyPEM   string
	ValidateExp    bool
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
	// Load local env file if present (for dev)
	_ = godotenv.Load("env.local")
	_ = godotenv.Load("./attchat-gateway-websocket/env.local")

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
			PublicKeyPEM:   viper.GetString("jwt.public_key"),
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

	// Allow loading public key from file if provided
	if path := strings.TrimSpace(viper.GetString("jwt.public_key_file")); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			fmt.Printf("[DEBUG] loaded jwt.public_key_file=%s len=%d\n", path, len(data))
			cfg.JWT.PublicKeyPEM = string(data)
		} else {
			fmt.Printf("[DEBUG] failed to read jwt.public_key_file=%s err=%v\n", path, err)
			return nil, err
		}
	}
	// Fallback: if still empty, try local defaults
	if cfg.JWT.PublicKeyPEM == "" {
		if data, err := os.ReadFile("jwt_dev_public.pem"); err == nil {
			fmt.Printf("[DEBUG] loaded jwt_dev_public.pem from CWD len=%d\n", len(data))
			cfg.JWT.PublicKeyPEM = string(data)
		} else if data, err := os.ReadFile("./attchat-gateway-websocket/jwt_dev_public.pem"); err == nil {
			fmt.Printf("[DEBUG] loaded ./attchat-gateway-websocket/jwt_dev_public.pem len=%d\n", len(data))
			cfg.JWT.PublicKeyPEM = string(data)
		} else {
			fmt.Printf("[DEBUG] could not find jwt_dev_public.pem in fallback locations\n")
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		fmt.Printf("[DEBUG] cwd=%s\n", cwd)
	}
	fmt.Printf("[DEBUG] final jwt.public_key length=%d\n", len(cfg.JWT.PublicKeyPEM))

	// Normalize streams: support comma-separated env
	if len(cfg.NATS.Streams) == 1 && strings.Contains(cfg.NATS.Streams[0], ",") {
		parts := strings.Split(cfg.NATS.Streams[0], ",")
		var cleaned []string
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				cleaned = append(cleaned, s)
			}
		}
		cfg.NATS.Streams = cleaned
	}

	return cfg, nil
}

func setDefaults() {
	// Server defaults
	viper.SetDefault("server.port", "8086")
	viper.SetDefault("server.read_timeout", "10s")
	viper.SetDefault("server.write_timeout", "10s")

	// JWT defaults
	viper.SetDefault("jwt.public_key", "")
	viper.SetDefault("jwt.public_key_file", "")
	viper.SetDefault("jwt.validate_exp", true)
	viper.SetDefault("jwt.allowed_issuers", []string{"attchat"})

	// NATS defaults
	viper.SetDefault("nats.url", "nats://localhost:4222")
	viper.SetDefault("nats.cluster_id", "attchat")
	viper.SetDefault("nats.client_id", "gateway")
	viper.SetDefault("nats.reconnect_wait", "2s")
	viper.SetDefault("nats.max_reconnects", -1) // Unlimited
	viper.SetDefault("nats.streams", []string{"CHAT", "NOTIFY", "ONLINE", "ANALYTICS", "AUDIT", "BILLING", "FILE", "EMAIL"})

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
