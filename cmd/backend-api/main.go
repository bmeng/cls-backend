package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/apahim/cls-backend/internal/api"
	"github.com/apahim/cls-backend/internal/config"
	"github.com/apahim/cls-backend/internal/database"
	"github.com/apahim/cls-backend/internal/messaging"
	"github.com/apahim/cls-backend/internal/reconciliation"
	"github.com/apahim/cls-backend/internal/utils"
	"go.uber.org/zap"
)

// Build information (set via ldflags)
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger component
	logger := utils.NewLogger("main")

	logger.Info("Starting CLS Backend API",
		zap.String("version", Version),
		zap.String("commit", GitCommit),
		zap.String("build_time", BuildTime),
		zap.String("environment", cfg.Server.Environment),
	)

	// Initialize database connection
	repo, err := database.NewRepository(cfg.Database)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer repo.Close()

	// Initialize messaging service (supports both GCP Pub/Sub and AWS SNS+SQS)
	messagingConfig, err := messaging.FromAppConfig(&cfg.Messaging)
	if err != nil {
		logger.Fatal("Failed to convert messaging configuration", zap.Error(err))
	}

	messagingProvider, err := messaging.NewProvider(messagingConfig)
	if err != nil {
		logger.Fatal("Failed to initialize messaging provider",
			zap.String("provider", cfg.Messaging.Provider),
			zap.Error(err))
	}
	defer messagingProvider.Stop()

	// Start messaging service
	if err := messagingProvider.Start(); err != nil {
		logger.Fatal("Failed to start messaging service",
			zap.String("provider", cfg.Messaging.Provider),
			zap.Error(err))
	}

	logger.Info("Messaging provider initialized successfully",
		zap.String("provider", cfg.Messaging.Provider))

	// Get the publisher instance
	publisher := messagingProvider.GetPublisher().(messaging.Publisher)

	// Initialize and start reconciliation scheduler
	scheduler := reconciliation.NewScheduler(repo, publisher, &cfg.Reconciliation)

	ctx := context.Background()
	if err := scheduler.Start(ctx); err != nil {
		logger.Fatal("Failed to start reconciliation scheduler", zap.Error(err))
	}
	defer scheduler.Stop()

	// Initialize and start reactive reconciler (database change-driven reconciliation)
	reactiveReconcilerConfig := reconciliation.DefaultReactiveReconciliationConfig()
	reactiveReconciler := reconciliation.NewReactiveReconciler(repo, publisher, &cfg.Database, reactiveReconcilerConfig)

	if err := reactiveReconciler.Start(ctx); err != nil {
		logger.Warn("Failed to start reactive reconciler", zap.Error(err))
		// Don't fail startup if reactive reconciler fails - it's an enhancement
	} else {
		logger.Info("Reactive reconciler started successfully")
	}
	defer func() {
		if err := reactiveReconciler.Stop(ctx); err != nil {
			logger.Error("Error stopping reactive reconciler", zap.Error(err))
		}
	}()

	// Initialize the simplified HTTP server
	server := api.NewServer(cfg, repo, messagingProvider)

	// Start server with context
	serverCtx, serverCancel := context.WithCancel(ctx)
	defer serverCancel()

	go func() {
		if err := server.Start(serverCtx); err != nil {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	logger.Info("Server started successfully", zap.Int("port", cfg.Server.Port))

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Graceful shutdown
	if err := server.Stop(); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited")
}