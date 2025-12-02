package aws

import (
	"context"
	"fmt"
	"sync"

	"github.com/apahim/cls-backend/internal/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"go.uber.org/zap"
)

// Config holds AWS-specific configuration
type Config struct {
	Region                string
	ClusterEventsTopicARN string
	AccessKeyID           string
	SecretAccessKey       string
	SessionToken          string
	UseIAMRole            bool
}

// Adapter implements the messaging.Provider interface for AWS SNS+SQS
type Adapter struct {
	snsClient *sns.Client
	publisher *Publisher
	logger    *utils.Logger
	config    *Config

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
	status string
}

// NewAdapter creates a new AWS messaging adapter using SNS+SQS
func NewAdapter(awsConfig *Config) (*Adapter, error) {
	logger := utils.NewLogger("aws_messaging_adapter")

	ctx, cancel := context.WithCancel(context.Background())

	// Load AWS SDK configuration
	var cfg aws.Config
	var err error

	if awsConfig.UseIAMRole {
		// Use IAM role (EC2/EKS instance profile)
		logger.Info("Using IAM role for AWS authentication")
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(awsConfig.Region),
		)
	} else {
		// Use explicit credentials
		logger.Info("Using explicit credentials for AWS authentication")
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(awsConfig.Region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				awsConfig.AccessKeyID,
				awsConfig.SecretAccessKey,
				awsConfig.SessionToken,
			)),
		)
	}

	if err != nil {
		cancel()
		logger.Error("Failed to load AWS configuration", zap.Error(err))
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create SNS client
	snsClient := sns.NewFromConfig(cfg)

	// Create publisher
	publisher := &Publisher{
		snsClient:  snsClient,
		topicARN:   awsConfig.ClusterEventsTopicARN,
		logger:     utils.NewLogger("aws_publisher"),
		ctx:        ctx,
	}

	adapter := &Adapter{
		snsClient: snsClient,
		publisher: publisher,
		logger:    logger,
		config:    awsConfig,
		ctx:       ctx,
		cancel:    cancel,
		status:    "initialized",
	}

	logger.Info("AWS messaging adapter created successfully",
		zap.String("region", awsConfig.Region),
		zap.String("topic_arn", awsConfig.ClusterEventsTopicARN),
	)

	return adapter, nil
}

// Start starts the messaging service
func (a *Adapter) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.status == "running" {
		return fmt.Errorf("service is already running")
	}

	a.logger.Info("Starting AWS messaging adapter")

	// Verify SNS topic exists
	if err := a.verifyTopic(); err != nil {
		return fmt.Errorf("failed to verify SNS topic: %w", err)
	}

	a.status = "running"
	a.logger.Info("AWS messaging adapter started successfully")
	return nil
}

// Stop stops the messaging service
func (a *Adapter) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.status != "running" {
		return fmt.Errorf("service is not running")
	}

	a.logger.Info("Stopping AWS messaging adapter")

	// Cancel context to stop all operations
	a.cancel()

	a.status = "stopped"
	a.logger.Info("AWS messaging adapter stopped successfully")
	return nil
}

// Health checks the health of the messaging service
func (a *Adapter) Health(ctx context.Context) (string, error) {
	a.mu.RLock()
	status := a.status
	a.mu.RUnlock()

	if status != "running" {
		return "unhealthy", fmt.Errorf("service is not running (status: %s)", status)
	}

	// Verify SNS connectivity by checking topic attributes
	_, err := a.snsClient.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{
		TopicArn: aws.String(a.config.ClusterEventsTopicARN),
	})

	if err != nil {
		return "unhealthy", fmt.Errorf("SNS health check failed: %w", err)
	}

	return "healthy", nil
}

// GetPublisher returns the publisher instance
func (a *Adapter) GetPublisher() any {
	return a.publisher
}

// IsRunning returns true if the service is running
func (a *Adapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status == "running"
}

// verifyTopic verifies that the SNS topic exists
func (a *Adapter) verifyTopic() error {
	a.logger.Info("Verifying SNS topic exists",
		zap.String("topic_arn", a.config.ClusterEventsTopicARN),
	)

	_, err := a.snsClient.GetTopicAttributes(a.ctx, &sns.GetTopicAttributesInput{
		TopicArn: aws.String(a.config.ClusterEventsTopicARN),
	})

	if err != nil {
		a.logger.Error("Failed to verify SNS topic",
			zap.String("topic_arn", a.config.ClusterEventsTopicARN),
			zap.Error(err),
		)
		return fmt.Errorf("SNS topic verification failed: %w", err)
	}

	a.logger.Info("SNS topic verified successfully")
	return nil
}
