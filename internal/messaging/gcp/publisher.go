package gcp

import (
	"context"

	"github.com/apahim/cls-backend/internal/models"
	"github.com/apahim/cls-backend/internal/pubsub"
)

// PublisherAdapter wraps the GCP Pub/Sub publisher to implement messaging.Publisher
type PublisherAdapter struct {
	publisher *pubsub.Publisher
}

// PublishClusterEvent publishes a cluster lifecycle event
func (p *PublisherAdapter) PublishClusterEvent(ctx context.Context, eventType string, cluster *models.Cluster) error {
	return p.publisher.PublishClusterEvent(ctx, eventType, cluster)
}

// PublishNodePoolEvent publishes a nodepool lifecycle event
func (p *PublisherAdapter) PublishNodePoolEvent(ctx context.Context, eventType string, nodepool *models.NodePool) error {
	return p.publisher.PublishNodePoolEvent(ctx, eventType, nodepool)
}

// PublishReconciliationEvent publishes a reconciliation event
func (p *PublisherAdapter) PublishReconciliationEvent(ctx context.Context, event *models.ReconciliationEvent) error {
	return p.publisher.PublishReconciliationEvent(ctx, event)
}

// Convenience methods
func (p *PublisherAdapter) PublishClusterCreated(ctx context.Context, cluster *models.Cluster) error {
	return p.publisher.PublishClusterCreated(ctx, cluster)
}

func (p *PublisherAdapter) PublishClusterUpdated(ctx context.Context, cluster *models.Cluster) error {
	return p.publisher.PublishClusterUpdated(ctx, cluster)
}

func (p *PublisherAdapter) PublishClusterDeleted(ctx context.Context, cluster *models.Cluster) error {
	return p.publisher.PublishClusterDeleted(ctx, cluster)
}

func (p *PublisherAdapter) PublishNodePoolCreated(ctx context.Context, nodepool *models.NodePool) error {
	return p.publisher.PublishNodePoolCreated(ctx, nodepool)
}

func (p *PublisherAdapter) PublishNodePoolUpdated(ctx context.Context, nodepool *models.NodePool) error {
	return p.publisher.PublishNodePoolUpdated(ctx, nodepool)
}

func (p *PublisherAdapter) PublishNodePoolDeleted(ctx context.Context, nodepool *models.NodePool) error {
	return p.publisher.PublishNodePoolDeleted(ctx, nodepool)
}
