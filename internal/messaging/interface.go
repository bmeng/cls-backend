package messaging

import (
	"context"
	"time"

	"github.com/apahim/cls-backend/internal/models"
)

// Provider represents a cloud messaging provider implementation
type Provider interface {
	// Start initializes and starts the messaging service
	Start() error

	// Stop gracefully shuts down the messaging service
	Stop() error

	// Health checks the health of the messaging service
	Health(ctx context.Context) (string, error)

	// GetPublisher returns a Publisher instance for publishing events
	// Returns any type that implements the Publisher interface
	GetPublisher() any

	// IsRunning returns true if the service is running
	IsRunning() bool
}

// Publisher defines the interface for publishing messages to topics/queues
type Publisher interface {
	// PublishClusterEvent publishes a cluster lifecycle event
	PublishClusterEvent(ctx context.Context, eventType string, cluster *models.Cluster) error

	// PublishNodePoolEvent publishes a nodepool lifecycle event
	PublishNodePoolEvent(ctx context.Context, eventType string, nodepool *models.NodePool) error

	// PublishReconciliationEvent publishes a reconciliation event
	PublishReconciliationEvent(ctx context.Context, event *models.ReconciliationEvent) error

	// Convenience methods for common events
	PublishClusterCreated(ctx context.Context, cluster *models.Cluster) error
	PublishClusterUpdated(ctx context.Context, cluster *models.Cluster) error
	PublishClusterDeleted(ctx context.Context, cluster *models.Cluster) error
	PublishNodePoolCreated(ctx context.Context, nodepool *models.NodePool) error
	PublishNodePoolUpdated(ctx context.Context, nodepool *models.NodePool) error
	PublishNodePoolDeleted(ctx context.Context, nodepool *models.NodePool) error
}

// Message represents a generic message received from a messaging system
type Message struct {
	ID          string            `json:"id"`
	Data        []byte            `json:"data"`
	Attributes  map[string]string `json:"attributes"`
	PublishTime time.Time         `json:"publish_time"`
}

// MessageHandler defines the interface for handling messages
type MessageHandler interface {
	HandleMessage(ctx context.Context, message *Message) error
}

// ProviderType represents the type of messaging provider
type ProviderType string

const (
	ProviderTypeGCP ProviderType = "gcp"
	ProviderTypeAWS ProviderType = "aws"
)

// Config holds generic messaging configuration
type Config struct {
	Provider ProviderType
	GCP      *GCPConfig
	AWS      *AWSConfig
}

// GCPConfig holds GCP-specific configuration
type GCPConfig struct {
	ProjectID              string
	ClusterEventsTopic     string
	EmulatorHost           string
	CredentialsFile        string
	MaxConcurrentHandlers  int
	MaxOutstandingMessages int
}

// AWSConfig holds AWS-specific configuration
type AWSConfig struct {
	Region                string
	ClusterEventsTopicARN string // SNS Topic ARN
	AccessKeyID           string
	SecretAccessKey       string
	SessionToken          string // Optional for temporary credentials
	UseIAMRole            bool   // Use EC2/EKS IAM role instead of credentials
}
