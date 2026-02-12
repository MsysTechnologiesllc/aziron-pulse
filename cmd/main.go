package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/grafana/pyroscope-go"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/db"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/handlers"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/k8s"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/middleware"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/service"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
)

var (
	Version   = "v1.0.0"
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

	ctx := context.Background()

	// Initialize OpenTelemetry
	otelEndpoint := getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318")
	shutdown, err := telemetry.InitTelemetry("aziron-pulse", otelEndpoint)
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

	// Initialize Pulse metrics
	telemetry.InitPulseMetrics()

	// Initialize Pyroscope profiling
	pyroscopeURL := getEnv("PYROSCOPE_SERVER_URL", "http://localhost:4040")
	if pyroscopeURL != "" {
		profiler, err := pyroscope.Start(pyroscope.Config{
			ApplicationName: "aziron-pulse",
			ServerAddress:   pyroscopeURL,
			Logger:          nil, // Use default logger
			Tags: map[string]string{
				"service":     "aziron-pulse",
				"version":     Version,
				"environment": getEnv("ENVIRONMENT", "development"),
			},
			ProfileTypes: []pyroscope.ProfileType{
				pyroscope.ProfileCPU,
				pyroscope.ProfileAllocObjects,
				pyroscope.ProfileAllocSpace,
				pyroscope.ProfileInuseObjects,
				pyroscope.ProfileInuseSpace,
				pyroscope.ProfileGoroutines,
				pyroscope.ProfileMutexCount,
				pyroscope.ProfileMutexDuration,
				pyroscope.ProfileBlockCount,
				pyroscope.ProfileBlockDuration,
			},
		})
		if err != nil {
			logger.Warn("Failed to initialize Pyroscope", zap.Error(err))
		} else {
			defer func() {
				if err := profiler.Stop(); err != nil {
					logger.Error("Failed to stop Pyroscope", zap.Error(err))
				}
			}()
			logger.Info("Pyroscope profiling initialized successfully", zap.String("server", pyroscopeURL))
		}
	}

	// Initialize cost calculator
	telemetry.InitCostCalculator()
	logger.Info("Cost calculator initialized successfully")
	logger.Info("Pulse metrics initialized successfully")

	// Initialize database (using same credentials as aziron-server)
	dbConfig := db.Config{
		Host:     getEnv("DB_HOST", "127.0.0.1"),
		Port:     getEnvInt("DB_PORT", 5432),
		User:     getEnv("DB_USER", "aziron"),
		Password: getEnv("DB_PASSWORD", "aziron123"),
		DBName:   getEnv("DB_NAME", "aziron"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	}

	database, err := db.Connect(dbConfig)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer database.Close()
	logger.Info("Database connected successfully")

	// Initialize Kubernetes client
	k8sClient, err := k8s.NewClient(logger)
	if err != nil {
		logger.Fatal("Failed to create Kubernetes client", zap.Error(err))
	}
	logger.Info("Kubernetes client initialized successfully")

	// Initialize metrics watcher
	namespace := getEnv("K8S_NAMESPACE", "aziron-pulse")
	metricsWatcher := k8s.NewMetricsWatcher(k8sClient.Clientset, k8sClient.MetricsClientset, namespace)
	
	// Start metrics watcher
	metricsCtx, metricsCancel := context.WithCancel(context.Background())
	defer metricsCancel()
	if err := metricsWatcher.Start(metricsCtx); err != nil {
		logger.Warn("Failed to start metrics watcher", zap.Error(err))
	} else {
		defer metricsWatcher.Stop()
		logger.Info("Metrics watcher started successfully", zap.String("namespace", namespace))
	}

	// Initialize services
	workspaceRoot := getEnv("AZIRON_WORKSPACE", "/var/aziron/workspace")
	provisionService := service.NewProvisionService(database, k8sClient, workspaceRoot, logger)

	// Initialize TTL manager
	ttlInterval := time.Duration(getEnvInt("TTL_CHECK_INTERVAL", 5)) * time.Minute
	ttlManager := service.NewTTLManager(database, k8sClient, logger, ttlInterval)

	// Start TTL manager
	ttlCtx, ttlCancel := context.WithCancel(context.Background())
	defer ttlCancel()
	go ttlManager.Start(ttlCtx)

	// Initialize middleware
	jwtSecret := getEnv("JWT_SECRET", "your-secret-key")
	authMiddleware := middleware.NewAuthMiddleware(jwtSecret)
	requestContextMiddleware := middleware.NewRequestContextMiddleware()

	// Initialize handlers
	pulseHandler := handlers.NewPulseHandler()
	provisionHandler := handlers.NewProvisionHandler(provisionService, logger)
	proxyHandler := handlers.NewProxyHandler(provisionService, logger)

	// Create router
	router := mux.NewRouter()

	// Public health endpoints (no auth)
	router.HandleFunc("/health", pulseHandler.Health).Methods("GET")
	router.HandleFunc("/status", pulseHandler.Status).Methods("GET")
	router.Handle("/metrics", promhttp.Handler()).Methods("GET")

	// Root endpoint
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"service": "aziron-pulse",
			"version": "%s",
			"status": "running",
			"endpoints": {
				"health": "/health",
				"provision": "/provision",
				"proxy": "/pulse/{pulse_id}"
			}
		}`, Version)
	}).Methods("GET")

	// Protected routes (require authentication)
	protectedRouter := router.PathPrefix("").Subrouter()
	protectedRouter.Use(requestContextMiddleware.Enrich)
	protectedRouter.Use(authMiddleware.Authenticate)

	// Provision endpoints
	protectedRouter.HandleFunc("/provision", provisionHandler.ProvisionPod).Methods("POST")
	protectedRouter.HandleFunc("/provision", provisionHandler.ListPods).Methods("GET")
	protectedRouter.HandleFunc("/provision/{pulse_id}", provisionHandler.GetPod).Methods("GET")
	protectedRouter.HandleFunc("/provision/{pulse_id}", provisionHandler.DeletePod).Methods("DELETE")

	// Proxy endpoints (to code-server instances)
	protectedRouter.HandleFunc("/pulse/{pulse_id}/health", proxyHandler.HealthCheck).Methods("GET")
	protectedRouter.PathPrefix("/pulse/{pulse_id}").HandlerFunc(proxyHandler.ProxyToPod)

	// Legacy pulse heartbeat endpoint
	protectedRouter.HandleFunc("/pulse/heartbeat", pulseHandler.Heartbeat).Methods("POST")

	// Setup CORS
	c := cors.New(cors.Options{
		AllowOriginFunc: func(origin string) bool {
			return true
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
	port := getEnv("PULSE_PORT", "8081")

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

	// Stop TTL manager
	ttlCancel()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited")
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
