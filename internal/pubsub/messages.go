package pubsub

import (
	"context"
	"time"

	"github.com/apahim/cls-backend/internal/events"
)

// Message represents a Pub/Sub message
type Message struct {
	ID          string            `json:"id"`
	Data        []byte            `json:"data"`
	Attributes  map[string]string `json:"attributes"`
	PublishTime time.Time         `json:"publish_time"`
}

// MessageHandler defines the interface for handling Pub/Sub messages
type MessageHandler interface {
	HandleMessage(ctx context.Context, message *Message) error
}

// Re-export event types and functions from events package for backward compatibility
const (
	EventTypeClusterCreated  = events.EventTypeClusterCreated
	EventTypeClusterUpdated  = events.EventTypeClusterUpdated
	EventTypeClusterDeleted  = events.EventTypeClusterDeleted
	EventTypeNodePoolCreated = events.EventTypeNodePoolCreated
	EventTypeNodePoolUpdated = events.EventTypeNodePoolUpdated
	EventTypeNodePoolDeleted = events.EventTypeNodePoolDeleted
)

// Type aliases for backward compatibility
type ClusterEvent = events.ClusterEvent
type NodePoolEvent = events.NodePoolEvent

// Function aliases for backward compatibility
var NewClusterEvent = events.NewClusterEvent
var NewNodePoolEvent = events.NewNodePoolEvent
