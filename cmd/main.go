package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/handlers"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
)

var (
	Version   = "v0.1.0"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Warning: Failed to load .env file: %v\n", err)
	}

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("Starting Aziron Pulse Service",
		zap.String("version", Version),
		zap.String("build_time", BuildTime),
		zap.String("git_commit", GitCommit),
	)

	// Initialize OpenTelemetry
	ctx := context.Background()
	shutdown, err := telemetry.InitTelemetry(ctx, "aziron-pulse", Version)
	if err != nil {
		logger.Warn("Failed to initialize telemetry", zap.Error(err))
	} else {
		defer func() {
			if err := shutdown(ctx); err != nil {
				logger.Error("Failed to shutdown telemetry", zap.Error(err))
			}
		}()
		logger.Info("OpenTelemetry initialized successfully")
	}

	// Create router
	router := mux.NewRouter()

	// Initialize handlers
	pulseHandler := handlers.NewPulseHandler(logger)

	// Register routes with /pulse prefix
	pulseRouter := router.PathPrefix("/pulse").Subrouter()
	
	// Health endpoints
	pulseRouter.HandleFunc("/health", pulseHandler.Health).Methods("GET")
	pulseRouter.HandleFunc("/health/ready", pulseHandler.Ready).Methods("GET")
	pulseRouter.HandleFunc("/health/live", pulseHandler.Live).Methods("GET")
	
	// Metrics endpoint
	pulseRouter.Handle("/metrics", promhttp.Handler()).Methods("GET")
	
	// Business logic endpoints
	pulseRouter.HandleFunc("/status", pulseHandler.GetStatus).Methods("GET")
	pulseRouter.HandleFunc("/heartbeat", pulseHandler.Heartbeat).Methods("POST")
	pulseRouter.HandleFunc("/events", pulseHandler.GetEvents).Methods("GET")
	pulseRouter.HandleFunc("/events", pulseHandler.CreateEvent).Methods("POST")

	// Root endpoint (without /pulse prefix)
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"service": "aziron-pulse",
			"version": "%s",
			"status": "running",
			"endpoints": ["/pulse/health", "/pulse/status", "/pulse/metrics"]
		}`, Version)
	}).Methods("GET")

	// Setup CORS
	c := cors.New(cors.Options{
		AllowOriginFunc: func(origin string) bool {
			return true // Allow all origins
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
		ExposedHeaders:   []string{"Content-Length", "Content-Type", "Authorization"},
	})

	handler := c.Handler(router)

	// Wrap with OpenTelemetry instrumentation
	handler = otelhttp.NewHandler(handler, "aziron-pulse-http",
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			return fmt.Sprintf("%s %s", r.Method, r.URL.Path)
		}),
	)

	// Get port from environment
	port := os.Getenv("PULSE_PORT")
	if port == "" {
		port = "8081" // Default port
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("Starting HTTP server", zap.String("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	logger.Info("Aziron Pulse service started successfully", zap.String("port", port))

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited")
}
