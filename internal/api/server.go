package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/apahim/cls-backend/internal/config"
	"github.com/apahim/cls-backend/internal/database"
	"github.com/apahim/cls-backend/internal/messaging"
	"github.com/apahim/cls-backend/internal/middleware"
	"github.com/apahim/cls-backend/internal/services"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Server represents the HTTP server
type Server struct {
	config          *config.Config
	router          *gin.Engine
	logger          *zap.Logger
	repository      *database.Repository
	messaging       messaging.Provider
	clusterService  *services.ClusterService
	clusterHandler  *ClusterHandler
	nodepoolHandler *NodePoolHandler
	httpServer      *http.Server
}

// NewServer creates a new HTTP server
func NewServer(
	cfg *config.Config,
	repository *database.Repository,
	messagingProvider messaging.Provider,
) *Server {
	logger := zap.L().Named("api_server")

	// Initialize services
	clusterService := services.NewClusterService(repository, messagingProvider)

	// Initialize handlers
	clusterHandler := NewClusterHandler(clusterService, repository.Status)
	nodepoolHandler := NewNodePoolHandler(repository, messagingProvider)

	// Setup router
	router := setupRouter(cfg, clusterHandler, nodepoolHandler)

	server := &Server{
		config:          cfg,
		router:          router,
		logger:          logger,
		repository:      repository,
		messaging:       messagingProvider,
		clusterService:  clusterService,
		clusterHandler:  clusterHandler,
		nodepoolHandler: nodepoolHandler,
	}

	// Create HTTP server
	server.httpServer = &http.Server{
		Addr:           fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:        router,
		ReadTimeout:    time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:   time.Duration(cfg.Server.WriteTimeoutSeconds) * time.Second,
		IdleTimeout:    time.Duration(cfg.Server.IdleTimeoutSeconds) * time.Second,
		MaxHeaderBytes: cfg.Server.MaxHeaderBytes,
	}

	return server
}

// setupRouter configures the Gin router with all routes and middleware
func setupRouter(cfg *config.Config, clusterHandler *ClusterHandler, nodepoolHandler *NodePoolHandler) *gin.Engine {
	// Set Gin mode based on environment
	if cfg.Server.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Global middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(middleware.CORS())
	router.Use(middleware.RequestID())

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().UTC(),
			"service":   "cls-backend",
		})
	})

	// API versioning
	v1 := router.Group("/api/v1")

	// Authentication middleware for API routes
	if cfg.Auth.Enabled {
		v1.Use(middleware.AuthRequired(cfg))
	} else {
		// For development - use mock user context
		v1.Use(middleware.MockUserContext())
	}

	// Register cluster routes
	clusterHandler.RegisterRoutes(v1)

	// Register nodepool routes
	nodepoolHandler.RegisterRoutes(v1)

	return router
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info("Starting HTTP server",
		zap.String("address", s.httpServer.Addr),
		zap.String("environment", s.config.Server.Environment),
	)

	// Start server in a goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Failed to start server", zap.Error(err))
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	return s.Stop()
}

// Stop gracefully shuts down the HTTP server
func (s *Server) Stop() error {
	s.logger.Info("Shutting down HTTP server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger.Error("Failed to shutdown server gracefully", zap.Error(err))
		return err
	}

	s.logger.Info("HTTP server shutdown complete")
	return nil
}

// GetRouter returns the Gin router (useful for testing)
func (s *Server) GetRouter() *gin.Engine {
	return s.router
}

// GetClusterService returns the cluster service (useful for testing)
func (s *Server) GetClusterService() *services.ClusterService {
	return s.clusterService
}