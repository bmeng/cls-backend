package reconciliation

import (
	"context"
	"sync"
	"time"

	"github.com/apahim/cls-backend/internal/config"
	"github.com/apahim/cls-backend/internal/database"
	"github.com/apahim/cls-backend/internal/messaging"
	"github.com/apahim/cls-backend/internal/utils"
	"github.com/lib/pq"
	"go.uber.org/zap"
)

// ReactiveReconciliationConfig represents the configuration for reactive reconciliation
type ReactiveReconciliationConfig struct {
	Enabled              bool          `json:"enabled"`
	ChangeTypes          []string      `json:"change_types"`          // ["spec", "status", "controller_status"]
	DebounceInterval     time.Duration `json:"debounce_interval"`     // Prevent rapid-fire events
	MaxEventsPerMinute   int           `json:"max_events_per_minute"` // Rate limiting
	DatabasePollInterval time.Duration `json:"database_poll_interval"` // How often to check DB config (fan-out to all controllers)
}

// DefaultReactiveReconciliationConfig returns the default reactive reconciliation configuration
func DefaultReactiveReconciliationConfig() *ReactiveReconciliationConfig {
	return &ReactiveReconciliationConfig{
		Enabled:              false, // Disabled by default for safe rollout
		ChangeTypes:          []string{"spec", "status", "controller_status"},
		DebounceInterval:     2 * time.Second,
		MaxEventsPerMinute:   60,
		DatabasePollInterval: 30 * time.Second, // Check database config every 30 seconds (fan-out to all controllers)
	}
}

// ReactiveReconciler manages reactive reconciliation triggered by database changes
type ReactiveReconciler struct {
	repository       *database.Repository
	publisher        messaging.Publisher
	dbConfig         *config.DatabaseConfig
	config           *ReactiveReconciliationConfig
	logger           *utils.Logger
	databaseListener *DatabaseChangeListener

	// Internal state
	running  bool
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex

	// Statistics
	stats *ReactiveReconcilerStats
}

// ReactiveReconcilerStats tracks statistics for reactive reconciliation
type ReactiveReconcilerStats struct {
	mu                    sync.RWMutex
	EventsReceived        int64     `json:"events_received"`
	EventsPublished       int64     `json:"events_published"`
	EventsDebounced       int64     `json:"events_debounced"`
	EventsRateLimited     int64     `json:"events_rate_limited"`
	EventsErrored         int64     `json:"events_errored"`
	LastEventTime         time.Time `json:"last_event_time"`
	LastConfigUpdateTime  time.Time `json:"last_config_update_time"`
	DatabaseConfigEnabled bool      `json:"database_config_enabled"`
}

// NewReactiveReconciler creates a new reactive reconciler
func NewReactiveReconciler(repository *database.Repository, publisher messaging.Publisher, dbConfig *config.DatabaseConfig, config *ReactiveReconciliationConfig) *ReactiveReconciler {
	if config == nil {
		config = DefaultReactiveReconciliationConfig()
	}

	return &ReactiveReconciler{
		repository: repository,
		publisher:  publisher,
		dbConfig:   dbConfig,
		config:     config,
		logger:     utils.NewLogger("reactive_reconciler"),
		stopChan:   make(chan struct{}),
		stats:      &ReactiveReconcilerStats{},
	}
}

// Start starts the reactive reconciler
func (r *ReactiveReconciler) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return nil // Already running
	}

	r.logger.Info("Starting reactive reconciler")

	// Check initial database configuration
	if err := r.updateConfigFromDatabase(ctx); err != nil {
		r.logger.Warn("Failed to load initial database configuration", zap.Error(err))
	}

	// Only start if enabled (either in config or database)
	if !r.isEnabled() {
		r.logger.Info("Reactive reconciliation is disabled, not starting database listener")
		r.running = true // Mark as running even though listener is not started
		return nil
	}

	// Create and start database listener
	r.databaseListener = NewDatabaseChangeListener(r.dbConfig, r.publisher)
	if err := r.databaseListener.Start(ctx); err != nil {
		r.logger.Error("Failed to start database change listener", zap.Error(err))
		return err
	}

	r.running = true

	// Start configuration monitoring goroutine
	r.wg.Add(1)
	go r.configMonitorLoop(ctx)

	r.logger.Info("Reactive reconciler started successfully")
	return nil
}

// Stop stops the reactive reconciler
func (r *ReactiveReconciler) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return nil
	}

	r.logger.Info("Stopping reactive reconciler")
	r.running = false
	close(r.stopChan)

	// Stop database listener if running
	if r.databaseListener != nil {
		if err := r.databaseListener.Stop(ctx); err != nil {
			r.logger.Error("Error stopping database listener", zap.Error(err))
		}
	}

	// Wait for goroutines to finish
	r.wg.Wait()
	r.logger.Info("Reactive reconciler stopped")

	return nil
}

// IsRunning returns whether the reactive reconciler is running
func (r *ReactiveReconciler) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// isEnabled checks if reactive reconciliation is enabled (config or database)
func (r *ReactiveReconciler) isEnabled() bool {
	r.stats.mu.RLock()
	defer r.stats.mu.RUnlock()

	// Check application config or database config
	return r.config.Enabled || r.stats.DatabaseConfigEnabled
}

// configMonitorLoop monitors database configuration changes
func (r *ReactiveReconciler) configMonitorLoop(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(r.config.DatabasePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.updateConfigFromDatabase(ctx); err != nil {
				r.logger.Debug("Error updating config from database", zap.Error(err))
			}
		case <-r.stopChan:
			r.logger.Debug("Config monitor loop stopping")
			return
		case <-ctx.Done():
			r.logger.Debug("Config monitor loop stopping due to context cancellation")
			return
		}
	}
}

// updateConfigFromDatabase updates configuration from the database
func (r *ReactiveReconciler) updateConfigFromDatabase(ctx context.Context) error {
	// Query the database for reactive reconciliation configuration (fan-out approach)
	query := `
		SELECT enabled, change_types, debounce_interval, max_events_per_minute
		FROM reactive_reconciliation_config
		ORDER BY id DESC
		LIMIT 1`

	var enabled bool
	var changeTypes pq.StringArray
	var debounceInterval string
	var maxEventsPerMinute int

	err := r.repository.GetClient().QueryRowContext(ctx, query).Scan(
		&enabled,
		&changeTypes,
		&debounceInterval,
		&maxEventsPerMinute,
	)

	if err != nil {
		// If no config found, use defaults
		if err.Error() == "sql: no rows in result set" {
			r.logger.Debug("No reactive reconciliation config found in database, using defaults")
			enabled = false
		} else {
			return err
		}
	}

	// Update stats with database configuration
	r.stats.mu.Lock()
	previousEnabled := r.stats.DatabaseConfigEnabled
	r.stats.DatabaseConfigEnabled = enabled
	r.stats.LastConfigUpdateTime = time.Now()
	r.stats.mu.Unlock()

	// Handle enabling/disabling database listener based on config changes
	if enabled != previousEnabled {
		r.logger.Info("Reactive reconciliation database config changed",
			zap.Bool("previous_enabled", previousEnabled),
			zap.Bool("new_enabled", enabled))

		if enabled && r.databaseListener == nil {
			// Enable: start database listener
			r.databaseListener = NewDatabaseChangeListener(r.dbConfig, r.publisher)
			if err := r.databaseListener.Start(ctx); err != nil {
				r.logger.Error("Failed to start database listener after config change", zap.Error(err))
			} else {
				r.logger.Info("Database listener started after config change")
			}
		} else if !enabled && r.databaseListener != nil {
			// Disable: stop database listener
			if err := r.databaseListener.Stop(ctx); err != nil {
				r.logger.Error("Failed to stop database listener after config change", zap.Error(err))
			} else {
				r.logger.Info("Database listener stopped after config change")
				r.databaseListener = nil
			}
		}
	}

	return nil
}

// GetStats returns statistics about the reactive reconciler
func (r *ReactiveReconciler) GetStats() map[string]interface{} {
	r.stats.mu.RLock()
	defer r.stats.mu.RUnlock()

	stats := map[string]interface{}{
		"running":                    r.IsRunning(),
		"enabled":                    r.isEnabled(),
		"config_enabled":             r.config.Enabled,
		"database_config_enabled":    r.stats.DatabaseConfigEnabled,
		"events_received":            r.stats.EventsReceived,
		"events_published":           r.stats.EventsPublished,
		"events_debounced":           r.stats.EventsDebounced,
		"events_rate_limited":        r.stats.EventsRateLimited,
		"events_errored":             r.stats.EventsErrored,
		"last_event_time":            r.stats.LastEventTime,
		"last_config_update_time":    r.stats.LastConfigUpdateTime,
		"debounce_interval":          r.config.DebounceInterval.String(),
		"max_events_per_minute":      r.config.MaxEventsPerMinute,
		"database_poll_interval":     r.config.DatabasePollInterval.String(),
	}

	// Add database listener stats if available
	if r.databaseListener != nil {
		listenerStats := r.databaseListener.GetStats()
		for k, v := range listenerStats {
			stats["listener_"+k] = v
		}
	}

	return stats
}

// EnableReactiveReconciliation enables reactive reconciliation in the database
func (r *ReactiveReconciler) EnableReactiveReconciliation(ctx context.Context) error {
	query := `SELECT enable_reactive_reconciliation()`
	if _, err := r.repository.GetClient().ExecContext(ctx, query); err != nil {
		return err
	}

	r.logger.Info("Reactive reconciliation enabled in database")

	// Force config update
	return r.updateConfigFromDatabase(ctx)
}

// DisableReactiveReconciliation disables reactive reconciliation in the database
func (r *ReactiveReconciler) DisableReactiveReconciliation(ctx context.Context) error {
	query := `SELECT disable_reactive_reconciliation()`
	if _, err := r.repository.GetClient().ExecContext(ctx, query); err != nil {
		return err
	}

	r.logger.Info("Reactive reconciliation disabled in database")

	// Force config update
	return r.updateConfigFromDatabase(ctx)
}

// UpdateStats updates internal statistics (called by the database listener)
func (r *ReactiveReconciler) UpdateStats(eventType string) {
	r.stats.mu.Lock()
	defer r.stats.mu.Unlock()

	r.stats.LastEventTime = time.Now()

	switch eventType {
	case "received":
		r.stats.EventsReceived++
	case "published":
		r.stats.EventsPublished++
	case "debounced":
		r.stats.EventsDebounced++
	case "rate_limited":
		r.stats.EventsRateLimited++
	case "errored":
		r.stats.EventsErrored++
	}
}