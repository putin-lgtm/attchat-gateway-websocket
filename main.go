package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/attchat/attchat-gateway/internal/config"
	"github.com/attchat/attchat-gateway/internal/metrics"
	"github.com/attchat/attchat-gateway/internal/nats"
	"github.com/attchat/attchat-gateway/internal/room"
	"github.com/attchat/attchat-gateway/internal/server"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Setup logger
	setupLogger()

	log.Info().Msg("Starting ATTChat Gateway...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	log.Info().
		Str("port", cfg.Server.Port).
		Str("nats_url", cfg.NATS.URL).
		Msg("Configuration loaded")

	// Initialize metrics
	metricsServer := metrics.NewServer(cfg.Metrics.Port)
	go metricsServer.Start()

	// Initialize room manager
	roomManager := room.NewManager()

	// Initialize NATS consumer
	natsConsumer, err := nats.NewConsumer(cfg.NATS, roomManager)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to NATS")
	}
	defer natsConsumer.Close()

	// Start NATS consumer
	go natsConsumer.Start()

	// Initialize and start HTTP/WebSocket server
	srv := server.New(cfg, roomManager, natsConsumer)
	go srv.Start()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}

	log.Info().Msg("Gateway stopped")
}

func setupLogger() {
	// Pretty console output for development
	if os.Getenv("APP_ENV") != "production" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	// Set log level
	level := os.Getenv("LOG_LEVEL")
	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
