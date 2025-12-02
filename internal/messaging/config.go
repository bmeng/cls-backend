package messaging

import (
	"github.com/apahim/cls-backend/internal/config"
)

// FromAppConfig converts the application's MessagingConfig to the messaging package's Config type
func FromAppConfig(cfg *config.MessagingConfig) (*Config, error) {
	providerType := ProviderType(cfg.Provider)

	messagingConfig := &Config{
		Provider: providerType,
	}

	switch providerType {
	case ProviderTypeGCP:
		messagingConfig.GCP = &GCPConfig{
			ProjectID:              cfg.GCP.ProjectID,
			ClusterEventsTopic:     cfg.GCP.ClusterEventsTopic,
			EmulatorHost:           cfg.GCP.EmulatorHost,
			CredentialsFile:        cfg.GCP.CredentialsFile,
			MaxConcurrentHandlers:  cfg.GCP.MaxConcurrentHandlers,
			MaxOutstandingMessages: cfg.GCP.MaxOutstandingMessages,
		}
	case ProviderTypeAWS:
		messagingConfig.AWS = &AWSConfig{
			Region:                cfg.AWS.Region,
			ClusterEventsTopicARN: cfg.AWS.ClusterEventsTopicARN,
			AccessKeyID:           cfg.AWS.AccessKeyID,
			SecretAccessKey:       cfg.AWS.SecretAccessKey,
			SessionToken:          cfg.AWS.SessionToken,
			UseIAMRole:            cfg.AWS.UseIAMRole,
		}
	}

	return messagingConfig, nil
}
