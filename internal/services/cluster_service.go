package services

import (
	"context"
	"fmt"
	"time"

	"github.com/apahim/cls-backend/internal/auth"
	"github.com/apahim/cls-backend/internal/database"
	"github.com/apahim/cls-backend/internal/messaging"
	"github.com/apahim/cls-backend/internal/models"
	"github.com/apahim/cls-backend/internal/utils"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ClusterService provides business logic for cluster operations
type ClusterService struct {
	repository *database.Repository
	messaging  messaging.Provider
	logger     *utils.Logger
}

// NewClusterService creates a new cluster service
func NewClusterService(repository *database.Repository, messagingProvider messaging.Provider) *ClusterService {
	return &ClusterService{
		repository: repository,
		messaging:  messagingProvider,
		logger:     utils.NewLogger("cluster_service"),
	}
}

// CreateCluster creates a new cluster
func (s *ClusterService) CreateCluster(ctx context.Context, req *models.ClusterCreateRequest, userEmail string) (*models.Cluster, error) {
	s.logger.Info("Creating cluster",
		zap.String("cluster_name", req.Name),
		zap.String("user_email", userEmail),
	)

	// Note: For cluster creation, we'll check global uniqueness still,
	// but we could change this to per-user uniqueness if desired
	// For now, keeping global uniqueness to prevent conflicts
	// TODO: Consider if cluster names should be unique per user instead of globally

	// Create cluster
	cluster := &models.Cluster{
		ID:              uuid.New(),
		Name:            req.Name,
		TargetProjectID: req.TargetProjectID,
		CreatedBy:       userEmail,
		Generation:      1,
		ResourceVersion: uuid.New().String(),
		Spec:            req.Spec,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// Use transaction to ensure cluster creation and event publishing are atomic
	err := s.repository.Transaction(ctx, func(txRepo *database.Repository) error {
		// Create cluster
		if err := txRepo.Clusters.Create(ctx, cluster); err != nil {
			return fmt.Errorf("failed to create cluster: %w", err)
		}

		// Publish cluster creation event
		if s.messaging != nil && s.messaging.IsRunning() {
			publisher := s.messaging.GetPublisher().(messaging.Publisher)
			if err := publisher.PublishClusterCreated(ctx, cluster); err != nil {
				s.logger.Warn("Failed to publish cluster creation event",
					zap.String("cluster_id", cluster.ID.String()),
					zap.Error(err),
				)
				// Don't fail the operation for event publishing failure
			}
		}

		return nil
	})

	if err != nil {
		s.logger.Error("Failed to create cluster",
			zap.String("cluster_name", req.Name),
			zap.String("user_email", userEmail),
			zap.Error(err),
		)
		return nil, err
	}

	s.logger.Info("Successfully created cluster",
		zap.String("cluster_id", cluster.ID.String()),
		zap.String("cluster_name", cluster.Name),
		zap.String("user_email", userEmail),
	)

	return cluster, nil
}

// GetCluster gets a cluster by ID with client isolation
func (s *ClusterService) GetCluster(ctx context.Context, clusterID uuid.UUID, userEmail string) (*models.Cluster, error) {
	s.logger.Info("Getting cluster",
		zap.String("cluster_id", clusterID.String()),
		zap.String("user_email", userEmail),
	)

	cluster, err := s.repository.Clusters.GetByID(ctx, clusterID, userEmail)
	if err != nil {
		if err == models.ErrClusterNotFound {
			s.logger.Info("Cluster not found",
				zap.String("cluster_id", clusterID.String()),
			)
			return nil, fmt.Errorf("cluster not found")
		}
		s.logger.Error("Failed to get cluster",
			zap.String("cluster_id", clusterID.String()),
			zap.Error(err),
		)
		return nil, err
	}

	s.logger.Info("Successfully retrieved cluster",
		zap.String("cluster_id", clusterID.String()),
		zap.String("cluster_name", cluster.Name),
	)

	return cluster, nil
}

// GetClusterByName gets a cluster by name
func (s *ClusterService) GetClusterByName(ctx context.Context, name string, userEmail string) (*models.Cluster, error) {
	s.logger.Info("Getting cluster by name",
		zap.String("cluster_name", name),
		zap.String("user_email", userEmail),
	)

	cluster, err := s.repository.Clusters.GetByName(ctx, name, userEmail)
	if err != nil {
		if err == models.ErrClusterNotFound {
			s.logger.Info("Cluster not found",
				zap.String("cluster_name", name),
			)
			return nil, fmt.Errorf("cluster not found")
		}
		s.logger.Error("Failed to get cluster by name",
			zap.String("cluster_name", name),
			zap.Error(err),
		)
		return nil, err
	}

	s.logger.Info("Successfully retrieved cluster by name",
		zap.String("cluster_id", cluster.ID.String()),
		zap.String("cluster_name", cluster.Name),
	)

	return cluster, nil
}

// ListClusters lists clusters for a specific user with client isolation
func (s *ClusterService) ListClusters(ctx context.Context, userEmail string, limit, offset int) ([]*models.Cluster, int64, error) {
	s.logger.Info("Listing clusters",
		zap.String("user_email", userEmail),
		zap.Int("limit", limit),
		zap.Int("offset", offset),
	)

	opts := &models.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	clusters, err := s.repository.Clusters.List(ctx, userEmail, opts)
	if err != nil {
		s.logger.Error("Failed to list clusters",
			zap.String("user_email", userEmail),
			zap.Error(err),
		)
		return nil, 0, err
	}

	// Get total count for pagination
	total, err := s.repository.Clusters.Count(ctx, userEmail)
	if err != nil {
		s.logger.Error("Failed to count clusters",
			zap.Error(err),
		)
		return nil, 0, err
	}

	s.logger.Info("Successfully listed clusters",
		zap.Int("count", len(clusters)),
		zap.Int64("total", total),
	)

	return clusters, total, nil
}

// ListClustersByCreatedBy lists clusters created by a specific user (for future authorization)
func (s *ClusterService) ListClustersByCreatedBy(ctx context.Context, createdBy string, limit, offset int) ([]*models.Cluster, int64, error) {
	s.logger.Info("Listing clusters by created_by",
		zap.String("created_by", createdBy),
		zap.Int("limit", limit),
		zap.Int("offset", offset),
	)

	opts := &models.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	clusters, err := s.repository.Clusters.ListByCreatedBy(ctx, createdBy, opts)
	if err != nil {
		s.logger.Error("Failed to list clusters by created_by",
			zap.String("created_by", createdBy),
			zap.Error(err),
		)
		return nil, 0, err
	}

	// Get total count for pagination
	total, err := s.repository.Clusters.CountByCreatedBy(ctx, createdBy)
	if err != nil {
		s.logger.Error("Failed to count clusters by created_by",
			zap.String("created_by", createdBy),
			zap.Error(err),
		)
		return nil, 0, err
	}

	s.logger.Info("Successfully listed clusters by created_by",
		zap.String("created_by", createdBy),
		zap.Int("count", len(clusters)),
		zap.Int64("total", total),
	)

	return clusters, total, nil
}

// UpdateCluster updates an existing cluster
func (s *ClusterService) UpdateCluster(ctx context.Context, clusterID uuid.UUID, req *models.ClusterUpdateRequest, userEmail string) (*models.Cluster, error) {
	s.logger.Info("Updating cluster",
		zap.String("cluster_id", clusterID.String()),
		zap.String("user_email", userEmail),
	)

	// First, get the existing cluster to ensure it exists and user owns it
	cluster, err := s.repository.Clusters.GetByID(ctx, clusterID, userEmail)
	if err != nil {
		if err == models.ErrClusterNotFound {
			s.logger.Info("Cluster not found for update",
				zap.String("cluster_id", clusterID.String()),
				zap.String("user_email", userEmail),
			)
			return nil, fmt.Errorf("cluster not found")
		}
		s.logger.Error("Failed to get cluster for update",
			zap.String("cluster_id", clusterID.String()),
			zap.String("user_email", userEmail),
			zap.Error(err),
		)
		return nil, err
	}

	// Update cluster fields
	cluster.Spec = req.Spec
	cluster.Generation++
	cluster.ResourceVersion = uuid.New().String()
	cluster.UpdatedAt = time.Now()

	// Use transaction to ensure cluster update and event publishing are atomic
	err = s.repository.Transaction(ctx, func(txRepo *database.Repository) error {
		// Update cluster with client isolation
		if err := txRepo.Clusters.Update(ctx, cluster, userEmail); err != nil {
			return fmt.Errorf("failed to update cluster: %w", err)
		}

		// Publish cluster update event
		if s.messaging != nil && s.messaging.IsRunning() {
			publisher := s.messaging.GetPublisher().(messaging.Publisher)
			if err := publisher.PublishClusterUpdated(ctx, cluster); err != nil {
				s.logger.Warn("Failed to publish cluster update event",
					zap.String("cluster_id", cluster.ID.String()),
					zap.Error(err),
				)
				// Don't fail the operation for event publishing failure
			}
		}

		return nil
	})

	if err != nil {
		s.logger.Error("Failed to update cluster",
			zap.String("cluster_id", clusterID.String()),
			zap.Error(err),
		)
		return nil, err
	}

	s.logger.Info("Successfully updated cluster",
		zap.String("cluster_id", cluster.ID.String()),
		zap.String("cluster_name", cluster.Name),
		zap.Int64("generation", cluster.Generation),
	)

	return cluster, nil
}

// DeleteCluster deletes a cluster
func (s *ClusterService) DeleteCluster(ctx context.Context, clusterID uuid.UUID, force bool, userEmail string) error {
	s.logger.Info("Deleting cluster",
		zap.String("cluster_id", clusterID.String()),
		zap.String("user_email", userEmail),
		zap.Bool("force", force),
	)

	// First, get the existing cluster to ensure it exists and user owns it
	cluster, err := s.repository.Clusters.GetByID(ctx, clusterID, userEmail)
	if err != nil {
		if err == models.ErrClusterNotFound {
			s.logger.Info("Cluster not found for deletion",
				zap.String("cluster_id", clusterID.String()),
				zap.String("user_email", userEmail),
			)
			return fmt.Errorf("cluster not found")
		}
		s.logger.Error("Failed to get cluster for deletion",
			zap.String("cluster_id", clusterID.String()),
			zap.String("user_email", userEmail),
			zap.Error(err),
		)
		return err
	}

	// Check if cluster is in a state that allows deletion (unless force is true)
	if !force && cluster.Status != nil && cluster.Status.Phase != "" &&
		cluster.Status.Phase != "Pending" && cluster.Status.Phase != "Failed" {
		s.logger.Warn("Cluster not in deletable state",
			zap.String("cluster_id", clusterID.String()),
			zap.String("status_phase", cluster.Status.Phase),
		)
		return fmt.Errorf("cluster must be in Pending or Failed state for deletion, use force=true to override")
	}

	// Use transaction to ensure cluster deletion and event publishing are atomic
	err = s.repository.Transaction(ctx, func(txRepo *database.Repository) error {
		// Soft delete cluster with client isolation
		if err := txRepo.Clusters.Delete(ctx, clusterID, userEmail); err != nil {
			return fmt.Errorf("failed to delete cluster: %w", err)
		}

		// Publish cluster deletion event
		if s.messaging != nil && s.messaging.IsRunning() {
			publisher := s.messaging.GetPublisher().(messaging.Publisher)
			if err := publisher.PublishClusterDeleted(ctx, cluster); err != nil {
				s.logger.Warn("Failed to publish cluster deletion event",
					zap.String("cluster_id", cluster.ID.String()),
					zap.Error(err),
				)
				// Don't fail the operation for event publishing failure
			}
		}

		return nil
	})

	if err != nil {
		s.logger.Error("Failed to delete cluster",
			zap.String("cluster_id", clusterID.String()),
			zap.Error(err),
		)
		return err
	}

	s.logger.Info("Successfully deleted cluster",
		zap.String("cluster_id", cluster.ID.String()),
		zap.String("cluster_name", cluster.Name),
		zap.Bool("force", force),
	)

	return nil
}

// Access control aware methods

// ListAllClusters lists all clusters (system-wide access for controllers)
func (s *ClusterService) ListAllClusters(ctx context.Context, limit, offset int) ([]*models.Cluster, int64, error) {
	s.logger.Info("Listing all clusters (system-wide)",
		zap.Int("limit", limit),
		zap.Int("offset", offset),
	)

	opts := &models.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	clusters, err := s.repository.Clusters.ListAll(ctx, opts)
	if err != nil {
		s.logger.Error("Failed to list all clusters",
			zap.Error(err),
		)
		return nil, 0, err
	}

	// Get total count for pagination
	total, err := s.repository.Clusters.CountAll(ctx)
	if err != nil {
		s.logger.Error("Failed to count all clusters",
			zap.Error(err),
		)
		return nil, 0, err
	}

	s.logger.Info("Successfully listed all clusters",
		zap.Int("count", len(clusters)),
		zap.Int64("total", total),
	)

	return clusters, total, nil
}

// GetClusterWithAccessControl gets a cluster with access control validation
func (s *ClusterService) GetClusterWithAccessControl(ctx context.Context, clusterID uuid.UUID, userCtx *auth.UserContext) (*models.Cluster, error) {
	s.logger.Info("Getting cluster with access control",
		zap.String("cluster_id", clusterID.String()),
		zap.String("user_email", userCtx.Email),
		zap.Bool("is_controller", userCtx.IsController),
	)

	var cluster *models.Cluster
	var err error

	if userCtx.IsController {
		// Controllers can access any cluster
		cluster, err = s.repository.Clusters.GetByIDWithoutFilter(ctx, clusterID)
	} else {
		// Users can only access their own clusters
		cluster, err = s.repository.Clusters.GetByID(ctx, clusterID, userCtx.Email)
	}

	if err != nil {
		if err == models.ErrClusterNotFound {
			s.logger.Info("Cluster not found or access denied",
				zap.String("cluster_id", clusterID.String()),
				zap.String("user_email", userCtx.Email),
				zap.Bool("is_controller", userCtx.IsController),
			)
			return nil, fmt.Errorf("cluster not found")
		}
		s.logger.Error("Failed to get cluster",
			zap.String("cluster_id", clusterID.String()),
			zap.Error(err),
		)
		return nil, err
	}

	// Additional access control check
	if !auth.CanAccessCluster(userCtx, cluster) {
		s.logger.Warn("Access denied to cluster",
			zap.String("cluster_id", clusterID.String()),
			zap.String("user_email", userCtx.Email),
			zap.String("cluster_created_by", cluster.CreatedBy),
		)
		return nil, fmt.Errorf("cluster not found")
	}

	s.logger.Info("Successfully retrieved cluster with access control",
		zap.String("cluster_id", clusterID.String()),
		zap.String("cluster_name", cluster.Name),
	)

	return cluster, nil
}

// UpdateClusterWithAccessControl updates a cluster with access control validation
func (s *ClusterService) UpdateClusterWithAccessControl(ctx context.Context, clusterID uuid.UUID, req *models.ClusterUpdateRequest, userCtx *auth.UserContext) (*models.Cluster, error) {
	s.logger.Info("Updating cluster with access control",
		zap.String("cluster_id", clusterID.String()),
		zap.String("user_email", userCtx.Email),
		zap.Bool("is_controller", userCtx.IsController),
	)

	// First, get the existing cluster to validate access
	cluster, err := s.GetClusterWithAccessControl(ctx, clusterID, userCtx)
	if err != nil {
		return nil, err
	}

	// Check if user can update this cluster
	if !auth.CanUpdateCluster(userCtx, cluster) {
		s.logger.Warn("User not authorized to update cluster",
			zap.String("cluster_id", clusterID.String()),
			zap.String("user_email", userCtx.Email),
			zap.String("cluster_created_by", cluster.CreatedBy),
		)
		return nil, fmt.Errorf("cluster not found")
	}

	// Update cluster fields
	cluster.Spec = req.Spec
	cluster.Generation++
	cluster.ResourceVersion = uuid.New().String()
	cluster.UpdatedAt = time.Now()

	// Use transaction to ensure cluster update and event publishing are atomic
	err = s.repository.Transaction(ctx, func(txRepo *database.Repository) error {
		// Update cluster
		var updateErr error
		if userCtx.IsController {
			updateErr = txRepo.Clusters.UpdateWithoutFilter(ctx, cluster)
		} else {
			updateErr = txRepo.Clusters.Update(ctx, cluster, userCtx.Email)
		}

		if updateErr != nil {
			return fmt.Errorf("failed to update cluster: %w", updateErr)
		}

		// Publish cluster update event
		if s.messaging != nil && s.messaging.IsRunning() {
			publisher := s.messaging.GetPublisher().(messaging.Publisher)
			if err := publisher.PublishClusterUpdated(ctx, cluster); err != nil {
				s.logger.Warn("Failed to publish cluster update event",
					zap.String("cluster_id", cluster.ID.String()),
					zap.Error(err),
				)
				// Don't fail the operation for event publishing failure
			}
		}

		return nil
	})

	if err != nil {
		s.logger.Error("Failed to update cluster",
			zap.String("cluster_id", clusterID.String()),
			zap.Error(err),
		)
		return nil, err
	}

	s.logger.Info("Successfully updated cluster with access control",
		zap.String("cluster_id", cluster.ID.String()),
		zap.String("cluster_name", cluster.Name),
		zap.Int64("generation", cluster.Generation),
	)

	return cluster, nil
}

// DeleteClusterWithAccessControl deletes a cluster with access control validation
func (s *ClusterService) DeleteClusterWithAccessControl(ctx context.Context, clusterID uuid.UUID, force bool, userCtx *auth.UserContext) error {
	s.logger.Info("Deleting cluster with access control",
		zap.String("cluster_id", clusterID.String()),
		zap.String("user_email", userCtx.Email),
		zap.Bool("is_controller", userCtx.IsController),
		zap.Bool("force", force),
	)

	// First, get the existing cluster to validate access
	cluster, err := s.GetClusterWithAccessControl(ctx, clusterID, userCtx)
	if err != nil {
		return err
	}

	// Check if user can delete this cluster
	if !auth.CanDeleteCluster(userCtx, cluster) {
		s.logger.Warn("User not authorized to delete cluster",
			zap.String("cluster_id", clusterID.String()),
			zap.String("user_email", userCtx.Email),
			zap.String("cluster_created_by", cluster.CreatedBy),
		)
		return fmt.Errorf("cluster not found")
	}

	// Check if cluster is in a state that allows deletion (unless force is true)
	if !force && cluster.Status != nil && cluster.Status.Phase != "" &&
		cluster.Status.Phase != "Pending" && cluster.Status.Phase != "Failed" {
		s.logger.Warn("Cluster not in deletable state",
			zap.String("cluster_id", clusterID.String()),
			zap.String("status_phase", cluster.Status.Phase),
		)
		return fmt.Errorf("cluster must be in Pending or Failed state for deletion, use force=true to override")
	}

	// Use transaction to ensure cluster deletion and event publishing are atomic
	err = s.repository.Transaction(ctx, func(txRepo *database.Repository) error {
		// Delete cluster
		var deleteErr error
		if userCtx.IsController {
			deleteErr = txRepo.Clusters.DeleteWithoutFilter(ctx, clusterID)
		} else {
			deleteErr = txRepo.Clusters.Delete(ctx, clusterID, userCtx.Email)
		}

		if deleteErr != nil {
			return fmt.Errorf("failed to delete cluster: %w", deleteErr)
		}

		// Publish cluster deletion event
		if s.messaging != nil && s.messaging.IsRunning() {
			publisher := s.messaging.GetPublisher().(messaging.Publisher)
			if err := publisher.PublishClusterDeleted(ctx, cluster); err != nil {
				s.logger.Warn("Failed to publish cluster deletion event",
					zap.String("cluster_id", cluster.ID.String()),
					zap.Error(err),
				)
				// Don't fail the operation for event publishing failure
			}
		}

		return nil
	})

	if err != nil {
		s.logger.Error("Failed to delete cluster",
			zap.String("cluster_id", clusterID.String()),
			zap.Error(err),
		)
		return err
	}

	s.logger.Info("Successfully deleted cluster with access control",
		zap.String("cluster_id", cluster.ID.String()),
		zap.String("cluster_name", cluster.Name),
		zap.Bool("force", force),
	)

	return nil
}