package gcp

import (
	"context"

	"github.com/apahim/cls-backend/internal/config"
	"github.com/apahim/cls-backend/internal/pubsub"
	"github.com/apahim/cls-backend/internal/utils"
	"go.uber.org/zap"
)

// Config holds GCP-specific configuration
type Config struct {
	ProjectID              string
	ClusterEventsTopic     string
	EmulatorHost           string
	CredentialsFile        string
	MaxConcurrentHandlers  int
	MaxOutstandingMessages int
}

// Adapter wraps the GCP Pub/Sub service to implement the messaging.Provider interface
type Adapter struct {
	service   *pubsub.Service
	publisher *PublisherAdapter
	logger    *utils.Logger
}

// NewAdapter creates a new GCP messaging adapter
func NewAdapter(gcpConfig *Config) (*Adapter, error) {
	logger := utils.NewLogger("gcp_messaging_adapter")

	// Convert to pubsub.Config format
	cfg := config.PubSubConfig{
		ProjectID:              gcpConfig.ProjectID,
		ClusterEventsTopic:     gcpConfig.ClusterEventsTopic,
		EmulatorHost:           gcpConfig.EmulatorHost,
		CredentialsFile:        gcpConfig.CredentialsFile,
		MaxConcurrentHandlers:  gcpConfig.MaxConcurrentHandlers,
		MaxOutstandingMessages: gcpConfig.MaxOutstandingMessages,
	}

	// Create the underlying Pub/Sub service
	service, err := pubsub.NewService(cfg)
	if err != nil {
		logger.Error("Failed to create GCP Pub/Sub service", zap.Error(err))
		return nil, err
	}

	// Wrap the publisher
	publisher := &PublisherAdapter{
		publisher: service.GetPublisher(),
	}

	adapter := &Adapter{
		service:   service,
		publisher: publisher,
		logger:    logger,
	}

	logger.Info("GCP messaging adapter created successfully")
	return adapter, nil
}

// Start starts the messaging service
func (a *Adapter) Start() error {
	a.logger.Info("Starting GCP messaging adapter")
	return a.service.Start()
}

// Stop stops the messaging service
func (a *Adapter) Stop() error {
	a.logger.Info("Stopping GCP messaging adapter")
	return a.service.Stop()
}

// Health checks the health of the messaging service
func (a *Adapter) Health(ctx context.Context) (string, error) {
	return a.service.Health(ctx)
}

// GetPublisher returns the publisher instance
func (a *Adapter) GetPublisher() any {
	return a.publisher
}

// IsRunning returns true if the service is running
func (a *Adapter) IsRunning() bool {
	return a.service.IsRunning()
}
