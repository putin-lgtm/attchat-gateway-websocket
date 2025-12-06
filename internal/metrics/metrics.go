package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

var (
	// Connection metrics
	ConnectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gateway_connections_total",
		Help: "Total number of WebSocket connections",
	})

	ConnectionsCurrent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateway_connections_current",
		Help: "Current number of active WebSocket connections",
	})

	// Room metrics
	RoomsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateway_rooms_total",
		Help: "Total number of active rooms",
	})

	// Message metrics
	MessagesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gateway_messages_received_total",
		Help: "Total number of messages received from clients",
	})

	MessagesSent = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gateway_messages_sent_total",
		Help: "Total number of messages sent to clients",
	})

	MessagesFromNATS = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gateway_messages_from_nats_total",
		Help: "Total number of messages received from NATS",
	})

	// Latency metrics
	MessageLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "gateway_message_latency_seconds",
		Help:    "Message processing latency in seconds",
		Buckets: []float64{.0001, .0005, .001, .005, .01, .025, .05, .1, .25, .5, 1},
	})

	// Error metrics
	ErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_errors_total",
		Help: "Total number of errors by type",
	}, []string{"type"})

	// Auth metrics
	AuthSuccess = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gateway_auth_success_total",
		Help: "Total number of successful authentications",
	})

	AuthFailure = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gateway_auth_failure_total",
		Help: "Total number of failed authentications",
	})
)

// Server is the metrics HTTP server
type Server struct {
	port string
}

// NewServer creates a new metrics server
func NewServer(port string) *Server {
	return &Server{port: port}
}

// Start starts the metrics server
func (s *Server) Start() {
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Info().Str("port", s.port).Msg("Starting metrics server")
	if err := http.ListenAndServe(":"+s.port, nil); err != nil {
		log.Error().Err(err).Msg("Metrics server error")
	}
}

