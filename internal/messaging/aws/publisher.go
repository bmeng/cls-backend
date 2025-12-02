package aws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/apahim/cls-backend/internal/events"
	"github.com/apahim/cls-backend/internal/models"
	"github.com/apahim/cls-backend/internal/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	"go.uber.org/zap"
)

// Publisher implements messaging.Publisher for AWS SNS
type Publisher struct {
	snsClient *sns.Client
	topicARN  string
	logger    *utils.Logger
	ctx       context.Context
}

// publishMessage is a helper to publish a message to SNS with attributes
func (p *Publisher) publishMessage(ctx context.Context, data []byte, attributes map[string]string) error {
	// Convert attributes to SNS message attributes
	messageAttributes := make(map[string]types.MessageAttributeValue)
	for key, value := range attributes {
		messageAttributes[key] = types.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(value),
		}
	}

	// Publish to SNS
	input := &sns.PublishInput{
		TopicArn:          aws.String(p.topicARN),
		Message:           aws.String(string(data)),
		MessageAttributes: messageAttributes,
	}

	result, err := p.snsClient.Publish(ctx, input)
	if err != nil {
		p.logger.Error("Failed to publish message to SNS",
			zap.String("topic_arn", p.topicARN),
			zap.Error(err),
		)
		return fmt.Errorf("failed to publish to SNS: %w", err)
	}

	p.logger.Debug("Message published successfully to SNS",
		zap.String("topic_arn", p.topicARN),
		zap.String("message_id", *result.MessageId),
		zap.Int("data_size", len(data)),
	)

	return nil
}

// PublishClusterEvent publishes a cluster lifecycle event
func (p *Publisher) PublishClusterEvent(ctx context.Context, eventType string, cluster *models.Cluster) error {
	event := events.NewClusterEvent(eventType, cluster.ID, cluster.Generation)

	data, err := event.ToJSON()
	if err != nil {
		p.logger.Error("Failed to serialize cluster event",
			zap.String("event_type", eventType),
			zap.String("cluster_id", cluster.ID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("failed to serialize cluster event: %w", err)
	}

	err = p.publishMessage(ctx, data, event.GetAttributes())
	if err != nil {
		p.logger.Error("Failed to publish cluster event",
			zap.String("event_type", eventType),
			zap.String("cluster_id", cluster.ID.String()),
			zap.Error(err),
		)
		return err
	}

	p.logger.Info("Cluster event published successfully to SNS",
		zap.String("event_type", eventType),
		zap.String("cluster_id", cluster.ID.String()),
		zap.String("cluster_name", cluster.Name),
		zap.Int64("generation", cluster.Generation),
	)

	return nil
}

// PublishNodePoolEvent publishes a nodepool lifecycle event
func (p *Publisher) PublishNodePoolEvent(ctx context.Context, eventType string, nodepool *models.NodePool) error {
	event := events.NewNodePoolEvent(eventType, nodepool.ClusterID, nodepool.ID, nodepool.Generation)

	data, err := event.ToJSON()
	if err != nil {
		p.logger.Error("Failed to serialize nodepool event",
			zap.String("event_type", eventType),
			zap.String("nodepool_id", nodepool.ID.String()),
			zap.Error(err),
		)
		return fmt.Errorf("failed to serialize nodepool event: %w", err)
	}

	err = p.publishMessage(ctx, data, event.GetAttributes())
	if err != nil {
		p.logger.Error("Failed to publish nodepool event",
			zap.String("event_type", eventType),
			zap.String("nodepool_id", nodepool.ID.String()),
			zap.Error(err),
		)
		return err
	}

	p.logger.Info("NodePool event published successfully to SNS",
		zap.String("event_type", eventType),
		zap.String("cluster_id", nodepool.ClusterID.String()),
		zap.String("nodepool_id", nodepool.ID.String()),
		zap.String("nodepool_name", nodepool.Name),
		zap.Int64("generation", nodepool.Generation),
	)

	return nil
}

// PublishReconciliationEvent publishes a reconciliation event
func (p *Publisher) PublishReconciliationEvent(ctx context.Context, event *models.ReconciliationEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		p.logger.Error("Failed to serialize reconciliation event",
			zap.String("cluster_id", event.ClusterID),
			zap.String("reason", event.Reason),
			zap.Error(err),
		)
		return fmt.Errorf("failed to serialize reconciliation event: %w", err)
	}

	// Create attributes for filtering
	attributes := map[string]string{
		"event_type": event.Type,
		"reason":     event.Reason,
		"cluster_id": event.ClusterID,
	}

	err = p.publishMessage(ctx, data, attributes)
	if err != nil {
		p.logger.Error("Failed to publish reconciliation event",
			zap.String("cluster_id", event.ClusterID),
			zap.String("reason", event.Reason),
			zap.Error(err),
		)
		return err
	}

	p.logger.Debug("Reconciliation event published successfully to SNS (fan-out)",
		zap.String("cluster_id", event.ClusterID),
		zap.String("reason", event.Reason),
	)

	return nil
}

// Convenience methods for common events

func (p *Publisher) PublishClusterCreated(ctx context.Context, cluster *models.Cluster) error {
	return p.PublishClusterEvent(ctx, events.EventTypeClusterCreated, cluster)
}

func (p *Publisher) PublishClusterUpdated(ctx context.Context, cluster *models.Cluster) error {
	return p.PublishClusterEvent(ctx, events.EventTypeClusterUpdated, cluster)
}

func (p *Publisher) PublishClusterDeleted(ctx context.Context, cluster *models.Cluster) error {
	return p.PublishClusterEvent(ctx, events.EventTypeClusterDeleted, cluster)
}

func (p *Publisher) PublishNodePoolCreated(ctx context.Context, nodepool *models.NodePool) error {
	return p.PublishNodePoolEvent(ctx, events.EventTypeNodePoolCreated, nodepool)
}

func (p *Publisher) PublishNodePoolUpdated(ctx context.Context, nodepool *models.NodePool) error {
	return p.PublishNodePoolEvent(ctx, events.EventTypeNodePoolUpdated, nodepool)
}

func (p *Publisher) PublishNodePoolDeleted(ctx context.Context, nodepool *models.NodePool) error {
	return p.PublishNodePoolEvent(ctx, events.EventTypeNodePoolDeleted, nodepool)
}
