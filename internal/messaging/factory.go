package messaging

import (
	"fmt"

	"github.com/apahim/cls-backend/internal/messaging/aws"
	"github.com/apahim/cls-backend/internal/messaging/gcp"
	"github.com/apahim/cls-backend/internal/utils"
	"go.uber.org/zap"
)

// NewProvider creates a new messaging provider based on the configuration
func NewProvider(config *Config) (Provider, error) {
	logger := utils.NewLogger("messaging_factory")

	logger.Info("Creating messaging provider",
		zap.String("provider", string(config.Provider)),
	)

	switch config.Provider {
	case ProviderTypeGCP:
		if config.GCP == nil {
			return nil, fmt.Errorf("GCP configuration is required when provider is 'gcp'")
		}
		// Convert messaging.GCPConfig to gcp.Config
		gcpConfig := &gcp.Config{
			ProjectID:              config.GCP.ProjectID,
			ClusterEventsTopic:     config.GCP.ClusterEventsTopic,
			EmulatorHost:           config.GCP.EmulatorHost,
			CredentialsFile:        config.GCP.CredentialsFile,
			MaxConcurrentHandlers:  config.GCP.MaxConcurrentHandlers,
			MaxOutstandingMessages: config.GCP.MaxOutstandingMessages,
		}
		return gcp.NewAdapter(gcpConfig)

	case ProviderTypeAWS:
		if config.AWS == nil {
			return nil, fmt.Errorf("AWS configuration is required when provider is 'aws'")
		}
		// Convert messaging.AWSConfig to aws.Config
		awsConfig := &aws.Config{
			Region:                config.AWS.Region,
			ClusterEventsTopicARN: config.AWS.ClusterEventsTopicARN,
			AccessKeyID:           config.AWS.AccessKeyID,
			SecretAccessKey:       config.AWS.SecretAccessKey,
			SessionToken:          config.AWS.SessionToken,
			UseIAMRole:            config.AWS.UseIAMRole,
		}
		return aws.NewAdapter(awsConfig)

	default:
		return nil, fmt.Errorf("unsupported messaging provider: %s (supported: gcp, aws)", config.Provider)
	}
}
