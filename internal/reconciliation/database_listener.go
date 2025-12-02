package reconciliation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/apahim/cls-backend/internal/config"
	"github.com/apahim/cls-backend/internal/messaging"
	"github.com/apahim/cls-backend/internal/models"
	"github.com/apahim/cls-backend/internal/utils"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
)

// DatabaseChangeNotification represents a change notification from the database (fan-out to all controllers)
type DatabaseChangeNotification struct {
	ClusterID  string  `json:"cluster_id"`
	ChangeType string  `json:"change_type"` // "spec", "status", "controller_status"
	Reason     string  `json:"reason"`
	Timestamp  float64 `json:"timestamp"`
}

// DatabaseChangeListener listens for database change notifications and triggers reconciliation
type DatabaseChangeListener struct {
	dbConfig   *config.DatabaseConfig
	publisher  messaging.Publisher
	logger     *utils.Logger
	conn       *pgx.Conn
	running    bool
	stopChan   chan struct{}
	wg         sync.WaitGroup
	mu         sync.Mutex

	// Debouncing to prevent rapid-fire events
	lastEventTime map[string]time.Time
	debounceMap   sync.RWMutex
}

// NewDatabaseChangeListener creates a new database change listener
func NewDatabaseChangeListener(dbConfig *config.DatabaseConfig, publisher messaging.Publisher) *DatabaseChangeListener {
	return &DatabaseChangeListener{
		dbConfig:      dbConfig,
		publisher:     publisher,
		logger:        utils.NewLogger("database_change_listener"),
		stopChan:      make(chan struct{}),
		lastEventTime: make(map[string]time.Time),
	}
}

// Start starts the database change listener
func (d *DatabaseChangeListener) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return nil // Already running
	}

	// Establish dedicated database connection for LISTEN
	conn, err := pgx.Connect(ctx, d.dbConfig.URL)
	if err != nil {
		return fmt.Errorf("failed to connect to database for listening: %w", err)
	}

	d.conn = conn
	d.running = true

	// Start listening for notifications
	if _, err := d.conn.Exec(ctx, "LISTEN reconcile_change"); err != nil {
		d.conn.Close(ctx)
		d.running = false
		return fmt.Errorf("failed to start listening for reconcile_change: %w", err)
	}

	d.logger.Info("Database change listener started, listening for reconcile_change notifications")

	// Start the listening goroutine
	d.wg.Add(1)
	go d.listenLoop(ctx)

	return nil
}

// Stop stops the database change listener
func (d *DatabaseChangeListener) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}

	d.logger.Info("Stopping database change listener")
	d.running = false
	close(d.stopChan)

	// Close database connection
	if d.conn != nil {
		d.conn.Close(ctx)
	}

	// Wait for goroutine to finish
	d.wg.Wait()
	d.logger.Info("Database change listener stopped")

	return nil
}

// IsRunning returns whether the listener is running
func (d *DatabaseChangeListener) IsRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

// listenLoop runs the main listening loop for database notifications
func (d *DatabaseChangeListener) listenLoop(ctx context.Context) {
	defer d.wg.Done()

	for {
		select {
		case <-d.stopChan:
			d.logger.Info("Database change listener loop stopping")
			return
		case <-ctx.Done():
			d.logger.Info("Database change listener loop stopping due to context cancellation")
			return
		default:
			// Wait for notification with timeout
			notification, err := d.conn.WaitForNotification(ctx)
			if err != nil {
				// Check if we should continue
				select {
				case <-d.stopChan:
					return
				case <-ctx.Done():
					return
				default:
					d.logger.Error("Error waiting for database notification", zap.Error(err))
					// Brief sleep to prevent tight loop on persistent errors
					time.Sleep(time.Second)
					continue
				}
			}

			if notification != nil {
				d.handleNotification(ctx, notification)
			}
		}
	}
}

// handleNotification processes a database change notification
func (d *DatabaseChangeListener) handleNotification(ctx context.Context, notification *pgconn.Notification) {
	d.logger.Debug("Received database change notification",
		zap.String("channel", notification.Channel),
		zap.String("payload", notification.Payload))

	// Parse the notification payload
	var changeNotification DatabaseChangeNotification
	if err := json.Unmarshal([]byte(notification.Payload), &changeNotification); err != nil {
		d.logger.Error("Failed to parse database change notification",
			zap.String("payload", notification.Payload),
			zap.Error(err))
		return
	}

	// Validate cluster ID
	clusterID, err := uuid.Parse(changeNotification.ClusterID)
	if err != nil {
		d.logger.Error("Invalid cluster ID in notification",
			zap.String("cluster_id", changeNotification.ClusterID),
			zap.Error(err))
		return
	}

	// Check debouncing
	if d.shouldDebounce(changeNotification) {
		d.logger.Debug("Debouncing database change notification",
			zap.String("cluster_id", changeNotification.ClusterID),
			zap.String("change_type", changeNotification.ChangeType),
			zap.String("reason", changeNotification.Reason))
		return
	}

	// Update debounce tracking
	d.updateDebounceTracking(changeNotification)

	// Publish single reconciliation event (fan-out to all controllers)
	if err := d.publishReconciliationEvent(ctx, clusterID, changeNotification); err != nil {
		d.logger.Error("Failed to publish reconciliation event",
			zap.String("cluster_id", changeNotification.ClusterID),
			zap.String("change_type", changeNotification.ChangeType),
			zap.Error(err))
	}
}

// shouldDebounce checks if an event should be debounced based on recent activity (fan-out approach)
func (d *DatabaseChangeListener) shouldDebounce(notification DatabaseChangeNotification) bool {
	d.debounceMap.RLock()
	defer d.debounceMap.RUnlock()

	// Create debounce key based on cluster and change type only (no controller type for fan-out)
	debounceKey := fmt.Sprintf("%s:%s", notification.ClusterID, notification.ChangeType)

	lastTime, exists := d.lastEventTime[debounceKey]
	if !exists {
		return false
	}

	// Check if enough time has passed (2 seconds default debounce)
	return time.Since(lastTime) < 2*time.Second
}

// updateDebounceTracking updates the debounce tracking for an event (fan-out approach)
func (d *DatabaseChangeListener) updateDebounceTracking(notification DatabaseChangeNotification) {
	d.debounceMap.Lock()
	defer d.debounceMap.Unlock()

	// Create debounce key based on cluster and change type only (no controller type for fan-out)
	debounceKey := fmt.Sprintf("%s:%s", notification.ClusterID, notification.ChangeType)

	d.lastEventTime[debounceKey] = time.Now()

	// Clean up old entries (older than 5 minutes)
	cutoffTime := time.Now().Add(-5 * time.Minute)
	for key, timestamp := range d.lastEventTime {
		if timestamp.Before(cutoffTime) {
			delete(d.lastEventTime, key)
		}
	}
}

// publishReconciliationEvent publishes a reconciliation event based on the database change (fan-out to all controllers)
func (d *DatabaseChangeListener) publishReconciliationEvent(ctx context.Context, clusterID uuid.UUID, notification DatabaseChangeNotification) error {
	// Get cluster info to obtain current generation
	cluster, err := d.getClusterInfo(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("failed to get cluster info: %w", err)
	}

	event := &models.ReconciliationEvent{
		Type:       "cluster.reconcile",
		ClusterID:  clusterID.String(),
		Reason:     notification.Reason,
		Generation: cluster.Generation, // Use actual cluster generation
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"scheduled_by":   "reactive_reconciliation",
			"change_type":    notification.ChangeType,
			"trigger_reason": notification.Reason,
			"triggered_at":   time.Unix(int64(notification.Timestamp), 0),
		},
	}

	if err := d.publisher.PublishReconciliationEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to publish reconciliation event: %w", err)
	}

	d.logger.Info("Published reactive reconciliation event (fan-out)",
		zap.String("cluster_id", clusterID.String()),
		zap.String("change_type", notification.ChangeType),
		zap.String("reason", notification.Reason))

	return nil
}

// getClusterInfo retrieves cluster information from the database
func (d *DatabaseChangeListener) getClusterInfo(ctx context.Context, clusterID uuid.UUID) (*models.Cluster, error) {
	// Note: This is a simplified version. In a real implementation, you might want to
	// inject a repository or use a more efficient way to get cluster info.
	// For now, we'll simulate the cluster data structure.

	// TODO: This should ideally use the repository pattern, but for simplicity
	// we'll create a minimal cluster struct with the required fields

	// Query cluster info directly from database connection
	query := `SELECT generation FROM clusters WHERE id = $1 AND deleted_at IS NULL`

	var generation int64

	err := d.conn.QueryRow(ctx, query, clusterID).Scan(&generation)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster info: %w", err)
	}

	return &models.Cluster{
		ID:         clusterID,
		Generation: generation,
	}, nil
}

// GetStats returns statistics about the database change listener
func (d *DatabaseChangeListener) GetStats() map[string]interface{} {
	d.debounceMap.RLock()
	defer d.debounceMap.RUnlock()

	stats := map[string]interface{}{
		"running":              d.IsRunning(),
		"active_debounce_keys": len(d.lastEventTime),
	}

	return stats
}