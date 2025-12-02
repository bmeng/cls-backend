package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Event types for Pub/Sub messages (simplified for fan-out architecture)
const (
	EventTypeClusterCreated  = "cluster.created"
	EventTypeClusterUpdated  = "cluster.updated"
	EventTypeClusterDeleted  = "cluster.deleted"
	EventTypeNodePoolCreated = "nodepool.created"
	EventTypeNodePoolUpdated = "nodepool.updated"
	EventTypeNodePoolDeleted = "nodepool.deleted"
)

// ClusterEvent represents a cluster lifecycle event (lightweight)
type ClusterEvent struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	ClusterID  uuid.UUID `json:"cluster_id"`
	Generation int64     `json:"generation"`
	Timestamp  time.Time `json:"timestamp"`
	Source     string    `json:"source"`
}

// NodePoolEvent represents a nodepool lifecycle event (lightweight)
type NodePoolEvent struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	ClusterID  uuid.UUID `json:"cluster_id"`
	NodePoolID uuid.UUID `json:"nodepool_id"`
	Generation int64     `json:"generation"`
	Timestamp  time.Time `json:"timestamp"`
	Source     string    `json:"source"`
}

// NewClusterEvent creates a new lightweight cluster event
func NewClusterEvent(eventType string, clusterID uuid.UUID, generation int64) *ClusterEvent {
	return &ClusterEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		ClusterID:  clusterID,
		Generation: generation,
		Timestamp:  time.Now(),
		Source:     "cls-backend",
	}
}

// NewNodePoolEvent creates a new lightweight nodepool event
func NewNodePoolEvent(eventType string, clusterID, nodepoolID uuid.UUID, generation int64) *NodePoolEvent {
	return &NodePoolEvent{
		ID:         uuid.New().String(),
		Type:       eventType,
		ClusterID:  clusterID,
		NodePoolID: nodepoolID,
		Generation: generation,
		Timestamp:  time.Now(),
		Source:     "cls-backend",
	}
}

// ToJSON serializes an event to JSON
func (e *ClusterEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// ToJSON serializes an event to JSON
func (e *NodePoolEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// GetAttributes returns message attributes for the event
func (e *ClusterEvent) GetAttributes() map[string]string {
	return map[string]string{
		"event_type": e.Type,
		"cluster_id": e.ClusterID.String(),
		"generation": fmt.Sprintf("%d", e.Generation),
		"source":     e.Source,
		"timestamp":  e.Timestamp.Format(time.RFC3339),
	}
}

// GetAttributes returns message attributes for the event
func (e *NodePoolEvent) GetAttributes() map[string]string {
	attrs := map[string]string{
		"event_type":  e.Type,
		"cluster_id":  e.ClusterID.String(),
		"nodepool_id": e.NodePoolID.String(),
		"generation":  fmt.Sprintf("%d", e.Generation),
		"source":      e.Source,
		"timestamp":   e.Timestamp.Format(time.RFC3339),
	}
	return attrs
}
