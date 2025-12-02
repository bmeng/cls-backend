package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the application
type Config struct {
	Server        ServerConfig
	Database      DatabaseConfig
	PubSub        PubSubConfig        // Deprecated: Use Messaging instead
	Messaging     MessagingConfig     // New unified messaging configuration
	Logging       LoggingConfig
	Auth          AuthConfig
	Reconciliation ReconciliationConfig
	Aggregation   AggregationConfig
	Metrics       MetricsConfig
}

// ReconciliationConfig holds reconciliation scheduler configuration
type ReconciliationConfig struct {
	Enabled                      bool          `mapstructure:"enabled"`
	CheckInterval                time.Duration `mapstructure:"check_interval"`
	MaxConcurrent                int           `mapstructure:"max_concurrent"`

	// Health-aware interval configuration
	AdaptiveEnabled              bool          `mapstructure:"adaptive_enabled"`
	HealthyInterval              time.Duration `mapstructure:"healthy_interval"`
	UnhealthyInterval            time.Duration `mapstructure:"unhealthy_interval"`
	DefaultInterval              time.Duration `mapstructure:"default_interval"`

	// Reactive reconciliation
	ReactiveEnabled              bool          `mapstructure:"reactive_enabled"`
	ReactiveDebounce             time.Duration `mapstructure:"reactive_debounce"`
	ReactiveMaxEventsPerMinute   int           `mapstructure:"reactive_max_events_per_minute"`
}

// AggregationConfig holds status aggregation configuration
type AggregationConfig struct {
	Enabled              bool          `mapstructure:"enabled"`
	Interval             time.Duration `mapstructure:"interval"`
	BatchSize            int           `mapstructure:"batch_size"`
	MaxConcurrency       int           `mapstructure:"max_concurrency"`
	RetryAttempts        int           `mapstructure:"retry_attempts"`
	RetryBackoff         time.Duration `mapstructure:"retry_backoff"`
	HealthCheckInterval  time.Duration `mapstructure:"health_check_interval"`
}

// MetricsConfig holds metrics server configuration
type MetricsConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port                 int           `mapstructure:"port"`
	Environment          string        `mapstructure:"environment"`
	ReadTimeoutSeconds   int           `mapstructure:"read_timeout_seconds"`
	WriteTimeoutSeconds  int           `mapstructure:"write_timeout_seconds"`
	IdleTimeoutSeconds   int           `mapstructure:"idle_timeout_seconds"`
	MaxHeaderBytes       int           `mapstructure:"max_header_bytes"`
	CorsAllowedOrigins   []string      `mapstructure:"cors_allowed_origins"`
}

// DatabaseConfig holds database connection configuration
type DatabaseConfig struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// PubSubConfig holds Cloud Pub/Sub configuration (simplified for fan-out architecture)
type PubSubConfig struct {
	ProjectID                  string `mapstructure:"project_id"`
	ClusterEventsTopic         string `mapstructure:"cluster_events_topic"`
	EmulatorHost               string `mapstructure:"emulator_host"`
	CredentialsFile            string `mapstructure:"credentials_file"`
	MaxConcurrentHandlers      int    `mapstructure:"max_concurrent_handlers"`
	MaxOutstandingMessages     int    `mapstructure:"max_outstanding_messages"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string
	Format string // "json" or "console"
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// MessagingConfig holds unified messaging configuration for both GCP and AWS
type MessagingConfig struct {
	Provider string `mapstructure:"provider"` // "gcp" or "aws"

	// GCP-specific configuration
	GCP GCPMessagingConfig

	// AWS-specific configuration
	AWS AWSMessagingConfig
}

// GCPMessagingConfig holds GCP Pub/Sub configuration
type GCPMessagingConfig struct {
	ProjectID              string `mapstructure:"project_id"`
	ClusterEventsTopic     string `mapstructure:"cluster_events_topic"`
	EmulatorHost           string `mapstructure:"emulator_host"`
	CredentialsFile        string `mapstructure:"credentials_file"`
	MaxConcurrentHandlers  int    `mapstructure:"max_concurrent_handlers"`
	MaxOutstandingMessages int    `mapstructure:"max_outstanding_messages"`
}

// AWSMessagingConfig holds AWS SNS+SQS configuration
type AWSMessagingConfig struct {
	Region                string `mapstructure:"region"`
	ClusterEventsTopicARN string `mapstructure:"cluster_events_topic_arn"`
	AccessKeyID           string `mapstructure:"access_key_id"`
	SecretAccessKey       string `mapstructure:"secret_access_key"`
	SessionToken          string `mapstructure:"session_token"`
	UseIAMRole            bool   `mapstructure:"use_iam_role"`
}


// Load loads configuration from environment variables with defaults
func Load() (*Config, error) {
	config := &Config{
		Server: ServerConfig{
			Port:                getIntEnv("PORT", 8080),
			Environment:         getEnv("ENVIRONMENT", "development"),
			ReadTimeoutSeconds:  getIntEnv("SERVER_READ_TIMEOUT_SECONDS", 30),
			WriteTimeoutSeconds: getIntEnv("SERVER_WRITE_TIMEOUT_SECONDS", 30),
			IdleTimeoutSeconds:  getIntEnv("SERVER_IDLE_TIMEOUT_SECONDS", 120),
			MaxHeaderBytes:      getIntEnv("SERVER_MAX_HEADER_BYTES", 1<<20), // 1MB default
			CorsAllowedOrigins:  getStringSliceEnv("CORS_ALLOWED_ORIGINS", []string{"*"}),
		},
		Database: DatabaseConfig{
			URL:             getEnv("DATABASE_URL", ""),
			MaxOpenConns:    getIntEnv("DATABASE_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getIntEnv("DATABASE_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getDurationEnv("DATABASE_CONN_MAX_LIFETIME", 5*time.Minute),
			ConnMaxIdleTime: getDurationEnv("DATABASE_CONN_MAX_IDLE_TIME", 1*time.Minute),
		},
		PubSub: PubSubConfig{
			ProjectID:              getEnv("GOOGLE_CLOUD_PROJECT", ""),
			ClusterEventsTopic:     getEnv("PUBSUB_CLUSTER_EVENTS_TOPIC", "cluster-events"),
			EmulatorHost:           getEnv("PUBSUB_EMULATOR_HOST", ""),
			CredentialsFile:        getEnv("GOOGLE_APPLICATION_CREDENTIALS", ""),
			MaxConcurrentHandlers:  getIntEnv("PUBSUB_MAX_CONCURRENT_HANDLERS", 10),
			MaxOutstandingMessages: getIntEnv("PUBSUB_MAX_OUTSTANDING_MESSAGES", 100),
		},
		Messaging: MessagingConfig{
			Provider: getEnv("MESSAGING_PROVIDER", "gcp"), // Default to GCP for backward compatibility
			GCP: GCPMessagingConfig{
				ProjectID:              getEnv("GOOGLE_CLOUD_PROJECT", ""),
				ClusterEventsTopic:     getEnv("PUBSUB_CLUSTER_EVENTS_TOPIC", "cluster-events"),
				EmulatorHost:           getEnv("PUBSUB_EMULATOR_HOST", ""),
				CredentialsFile:        getEnv("GOOGLE_APPLICATION_CREDENTIALS", ""),
				MaxConcurrentHandlers:  getIntEnv("PUBSUB_MAX_CONCURRENT_HANDLERS", 10),
				MaxOutstandingMessages: getIntEnv("PUBSUB_MAX_OUTSTANDING_MESSAGES", 100),
			},
			AWS: AWSMessagingConfig{
				Region:                getEnv("AWS_REGION", "us-east-1"),
				ClusterEventsTopicARN: getEnv("AWS_SNS_CLUSTER_EVENTS_TOPIC_ARN", ""),
				AccessKeyID:           getEnv("AWS_ACCESS_KEY_ID", ""),
				SecretAccessKey:       getEnv("AWS_SECRET_ACCESS_KEY", ""),
				SessionToken:          getEnv("AWS_SESSION_TOKEN", ""),
				UseIAMRole:            getBoolEnv("AWS_USE_IAM_ROLE", true), // Default to IAM role for security
			},
		},
		Logging: LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		Auth: AuthConfig{
			Enabled: !getBoolEnv("DISABLE_AUTH", false),
		},
		Reconciliation: ReconciliationConfig{
			Enabled:                    getBoolEnv("RECONCILIATION_ENABLED", true),
			CheckInterval:              getDurationEnv("RECONCILIATION_CHECK_INTERVAL", 1*time.Minute),
			MaxConcurrent:              getIntEnv("RECONCILIATION_MAX_CONCURRENT", 50),

			// Health-aware configuration
			AdaptiveEnabled:            getBoolEnv("RECONCILIATION_ADAPTIVE_ENABLED", true),
			HealthyInterval:            getDurationEnv("RECONCILIATION_HEALTHY_INTERVAL", 5*time.Minute),
			UnhealthyInterval:          getDurationEnv("RECONCILIATION_UNHEALTHY_INTERVAL", 30*time.Second),
			DefaultInterval:            getDurationEnv("RECONCILIATION_DEFAULT_INTERVAL", 5*time.Minute),

			// Reactive reconciliation
			ReactiveEnabled:            getBoolEnv("REACTIVE_RECONCILIATION_ENABLED", false),
			ReactiveDebounce:           getDurationEnv("REACTIVE_RECONCILIATION_DEBOUNCE", 2*time.Second),
			ReactiveMaxEventsPerMinute: getIntEnv("REACTIVE_RECONCILIATION_MAX_EVENTS_PER_MINUTE", 60),
		},
		Aggregation: AggregationConfig{
			Enabled:             getBoolEnv("AGGREGATION_ENABLED", true),
			Interval:            getDurationEnv("AGGREGATION_INTERVAL", 30*time.Second),
			BatchSize:           getIntEnv("AGGREGATION_BATCH_SIZE", 50),
			MaxConcurrency:      getIntEnv("AGGREGATION_MAX_CONCURRENCY", 10),
			RetryAttempts:       getIntEnv("AGGREGATION_RETRY_ATTEMPTS", 3),
			RetryBackoff:        getDurationEnv("AGGREGATION_RETRY_BACKOFF", 5*time.Second),
			HealthCheckInterval: getDurationEnv("AGGREGATION_HEALTH_CHECK_INTERVAL", 60*time.Second),
		},
		Metrics: MetricsConfig{
			Enabled: getBoolEnv("METRICS_ENABLED", true),
			Port:    getIntEnv("METRICS_PORT", 8081),
		},
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	// Validate messaging configuration based on provider
	switch c.Messaging.Provider {
	case "gcp":
		if c.Messaging.GCP.ProjectID == "" {
			return fmt.Errorf("GOOGLE_CLOUD_PROJECT is required when MESSAGING_PROVIDER=gcp")
		}
		if c.Messaging.GCP.ClusterEventsTopic == "" {
			return fmt.Errorf("PUBSUB_CLUSTER_EVENTS_TOPIC is required when MESSAGING_PROVIDER=gcp")
		}
	case "aws":
		if c.Messaging.AWS.Region == "" {
			return fmt.Errorf("AWS_REGION is required when MESSAGING_PROVIDER=aws")
		}
		if c.Messaging.AWS.ClusterEventsTopicARN == "" {
			return fmt.Errorf("AWS_SNS_CLUSTER_EVENTS_TOPIC_ARN is required when MESSAGING_PROVIDER=aws")
		}
		// Validate AWS credentials only if not using IAM role
		if !c.Messaging.AWS.UseIAMRole {
			if c.Messaging.AWS.AccessKeyID == "" || c.Messaging.AWS.SecretAccessKey == "" {
				return fmt.Errorf("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are required when AWS_USE_IAM_ROLE=false")
			}
		}
	default:
		return fmt.Errorf("invalid MESSAGING_PROVIDER: %s (must be 'gcp' or 'aws')", c.Messaging.Provider)
	}

	// Backward compatibility: validate PubSub config if still using old path
	if c.PubSub.ProjectID == "" && c.Messaging.Provider == "gcp" {
		return fmt.Errorf("GOOGLE_CLOUD_PROJECT is required")
	}

	return nil
}

// Helper functions for environment variable parsing

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getIntEnv(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getStringSliceEnv(key string, defaultValue []string) []string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		// Simple comma-separated parsing
		var result []string
		for _, item := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	}
	return defaultValue
}