package api

import (
	"net/http"
	"strconv"

	"github.com/apahim/cls-backend/internal/database"
	"github.com/apahim/cls-backend/internal/messaging"
	"github.com/apahim/cls-backend/internal/models"
	"github.com/apahim/cls-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// NodePoolHandler handles nodepool-related HTTP requests
type NodePoolHandler struct {
	repository *database.Repository
	messaging  messaging.Provider
	logger     *utils.Logger
}

// NewNodePoolHandler creates a new nodepool handler
func NewNodePoolHandler(repository *database.Repository, messagingProvider messaging.Provider) *NodePoolHandler {
	return &NodePoolHandler{
		repository: repository,
		messaging:  messagingProvider,
		logger:     utils.NewLogger("nodepool_handler"),
	}
}

// RegisterRoutes registers nodepool routes with the router
func (h *NodePoolHandler) RegisterRoutes(r *gin.RouterGroup) {
	nodepools := r.Group("/nodepools")
	{
		nodepools.POST("", h.CreateNodePool)
		nodepools.GET("", h.ListNodePools)
		nodepools.GET("/:id", h.GetNodePool)
		nodepools.PUT("/:id", h.UpdateNodePool)
		nodepools.DELETE("/:id", h.DeleteNodePool)
		nodepools.GET("/:id/status", h.GetNodePoolStatus)
		nodepools.PUT("/:id/status", h.UpdateNodePoolStatus)
	}
}

// CreateNodePool creates a new nodepool
func (h *NodePoolHandler) CreateNodePool(c *gin.Context) {
	var req models.NodePool
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Invalid request body",
			err.Error(),
		))
		return
	}

	// Validate required fields
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Validation failed",
			"nodepool name is required",
		))
		return
	}

	if req.ClusterID == uuid.Nil {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Validation failed",
			"cluster ID is required",
		))
		return
	}

	ctx := c.Request.Context()

	// Get user email from context for client isolation
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		h.logger.Error("No user email found in context")
		c.JSON(http.StatusUnauthorized, utils.NewAPIError(
			utils.ErrCodeUnauthorized,
			"Authentication required",
			"",
		))
		return
	}

	// Verify cluster exists
	_, err := h.repository.Clusters.GetByID(ctx, req.ClusterID, userEmail)
	if err != nil {
		if err == models.ErrClusterNotFound {
			c.JSON(http.StatusBadRequest, utils.NewAPIError(
				utils.ErrCodeValidation,
				"Invalid cluster ID",
				"cluster not found",
			))
			return
		}

		h.logger.Error("Failed to verify cluster",
			zap.String("cluster_id", req.ClusterID.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to verify cluster",
			err.Error(),
		))
		return
	}

	// Set initial values
	req.ID = uuid.New()
	req.Generation = 1
	req.ResourceVersion = uuid.New().String()

	// Create nodepool in database
	err = h.repository.NodePools.Create(ctx, &req)
	if err != nil {
		h.logger.Error("Failed to create nodepool",
			zap.String("nodepool_name", req.Name),
			zap.String("cluster_id", req.ClusterID.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to create nodepool",
			err.Error(),
		))
		return
	}

	// Publish nodepool created event
	if h.messaging != nil && h.messaging.IsRunning() {
		if err := h.messaging.GetPublisher().(messaging.Publisher).PublishNodePoolCreated(ctx, &req); err != nil {
			h.logger.Warn("Failed to publish nodepool created event",
				zap.String("nodepool_id", req.ID.String()),
				zap.Error(err),
			)
		}
	}

	h.logger.Info("NodePool created successfully",
		zap.String("nodepool_id", req.ID.String()),
		zap.String("nodepool_name", req.Name),
		zap.String("cluster_id", req.ClusterID.String()),
	)

	c.JSON(http.StatusCreated, req)
}

// ListNodePools lists nodepools with optional filtering
func (h *NodePoolHandler) ListNodePools(c *gin.Context) {
	// Parse query parameters
	opts := &models.ListOptions{
		Status: c.Query("status"),
		Health: c.Query("health"),
	}

	if limit := c.Query("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			opts.Limit = l
		}
	}

	if offset := c.Query("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			opts.Offset = o
		}
	}

	// Validate options
	if err := opts.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Invalid query parameters",
			err.Error(),
		))
		return
	}

	ctx := c.Request.Context()

	// Get nodepools
	nodepools, err := h.repository.NodePools.List(ctx, opts)
	if err != nil {
		h.logger.Error("Failed to list nodepools", zap.Error(err))
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to list nodepools",
			err.Error(),
		))
		return
	}

	// Get total count for pagination
	total, err := h.repository.NodePools.Count(ctx, opts)
	if err != nil {
		h.logger.Error("Failed to count nodepools", zap.Error(err))
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to count nodepools",
			err.Error(),
		))
		return
	}

	response := map[string]interface{}{
		"nodepools": nodepools,
		"total":     total,
		"limit":     opts.Limit,
		"offset":    opts.Offset,
	}

	c.JSON(http.StatusOK, response)
}

// GetNodePool retrieves a nodepool by ID
func (h *NodePoolHandler) GetNodePool(c *gin.Context) {
	idParam := c.Param("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Invalid nodepool ID",
			err.Error(),
		))
		return
	}

	ctx := c.Request.Context()

	// Get user email from context for client isolation
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		h.logger.Error("No user email found in context")
		c.JSON(http.StatusUnauthorized, utils.NewAPIError(
			utils.ErrCodeUnauthorized,
			"Authentication required",
			"",
		))
		return
	}

	nodepool, err := h.repository.NodePools.GetByID(ctx, id, userEmail)
	if err != nil {
		if err == models.ErrNodePoolNotFound {
			c.JSON(http.StatusNotFound, utils.NewAPIError(
				utils.ErrCodeNotFound,
				"NodePool not found",
				"",
			))
			return
		}

		h.logger.Error("Failed to get nodepool",
			zap.String("nodepool_id", id.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to get nodepool",
			err.Error(),
		))
		return
	}

	c.JSON(http.StatusOK, nodepool)
}

// UpdateNodePool updates an existing nodepool
func (h *NodePoolHandler) UpdateNodePool(c *gin.Context) {
	idParam := c.Param("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Invalid nodepool ID",
			err.Error(),
		))
		return
	}

	var req models.NodePool
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Invalid request body",
			err.Error(),
		))
		return
	}

	ctx := c.Request.Context()

	// Get user email from context for client isolation
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		h.logger.Error("No user email found in context")
		c.JSON(http.StatusUnauthorized, utils.NewAPIError(
			utils.ErrCodeUnauthorized,
			"Authentication required",
			"",
		))
		return
	}

	// Get existing nodepool
	existing, err := h.repository.NodePools.GetByID(ctx, id, userEmail)
	if err != nil {
		if err == models.ErrNodePoolNotFound {
			c.JSON(http.StatusNotFound, utils.NewAPIError(
				utils.ErrCodeNotFound,
				"NodePool not found",
				"",
			))
			return
		}

		h.logger.Error("Failed to get nodepool",
			zap.String("nodepool_id", id.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to get nodepool",
			err.Error(),
		))
		return
	}

	// Track changes for event publishing
	changes := map[string]interface{}{}
	if existing.Spec != req.Spec {
		changes["spec"] = map[string]interface{}{
			"old": existing.Spec,
			"new": req.Spec,
		}
	}

	// Update nodepool fields
	req.ID = id
	req.ClusterID = existing.ClusterID // Don't allow cluster ID changes
	req.Generation = existing.Generation + 1
	req.ResourceVersion = uuid.New().String()
	req.CreatedAt = existing.CreatedAt

	// Status is now managed via controller_status table, not via fields

	// Update nodepool in database
	err = h.repository.NodePools.Update(ctx, &req, userEmail)
	if err != nil {
		h.logger.Error("Failed to update nodepool",
			zap.String("nodepool_id", id.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to update nodepool",
			err.Error(),
		))
		return
	}

	// Publish nodepool updated event if there were changes
	if len(changes) > 0 && h.messaging != nil && h.messaging.IsRunning() {
		if err := h.messaging.GetPublisher().(messaging.Publisher).PublishNodePoolUpdated(ctx, &req); err != nil {
			h.logger.Warn("Failed to publish nodepool updated event",
				zap.String("nodepool_id", req.ID.String()),
				zap.Error(err),
			)
		}
	}

	h.logger.Info("NodePool updated successfully",
		zap.String("nodepool_id", req.ID.String()),
		zap.String("nodepool_name", req.Name),
		zap.Int64("generation", req.Generation),
	)

	c.JSON(http.StatusOK, req)
}

// DeleteNodePool deletes a nodepool
func (h *NodePoolHandler) DeleteNodePool(c *gin.Context) {
	idParam := c.Param("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Invalid nodepool ID",
			err.Error(),
		))
		return
	}

	ctx := c.Request.Context()

	// Get user email from context for client isolation
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		h.logger.Error("No user email found in context")
		c.JSON(http.StatusUnauthorized, utils.NewAPIError(
			utils.ErrCodeUnauthorized,
			"Authentication required",
			"",
		))
		return
	}

	// Get existing nodepool for event publishing
	nodepool, err := h.repository.NodePools.GetByID(ctx, id, userEmail)
	if err != nil {
		if err == models.ErrNodePoolNotFound {
			c.JSON(http.StatusNotFound, utils.NewAPIError(
				utils.ErrCodeNotFound,
				"NodePool not found",
				"",
			))
			return
		}

		h.logger.Error("Failed to get nodepool",
			zap.String("nodepool_id", id.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to get nodepool",
			err.Error(),
		))
		return
	}

	// Delete nodepool in database (soft delete)
	err = h.repository.NodePools.Delete(ctx, id, userEmail)
	if err != nil {
		h.logger.Error("Failed to delete nodepool",
			zap.String("nodepool_id", id.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to delete nodepool",
			err.Error(),
		))
		return
	}

	// Delete controller status
	if err := h.repository.Status.DeleteAllNodePoolControllerStatus(ctx, id); err != nil {
		h.logger.Warn("Failed to delete nodepool controller status",
			zap.String("nodepool_id", id.String()),
			zap.Error(err),
		)
	}

	// Publish nodepool deleted event
	if h.messaging != nil && h.messaging.IsRunning() {
		if err := h.messaging.GetPublisher().(messaging.Publisher).PublishNodePoolDeleted(ctx, nodepool); err != nil {
			h.logger.Warn("Failed to publish nodepool deleted event",
				zap.String("nodepool_id", nodepool.ID.String()),
				zap.Error(err),
			)
		}
	}

	h.logger.Info("NodePool deleted successfully",
		zap.String("nodepool_id", id.String()),
		zap.String("nodepool_name", nodepool.Name),
	)

	c.JSON(http.StatusNoContent, nil)
}

// GetNodePoolStatus retrieves nodepool status information
func (h *NodePoolHandler) GetNodePoolStatus(c *gin.Context) {
	idParam := c.Param("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Invalid nodepool ID",
			err.Error(),
		))
		return
	}

	ctx := c.Request.Context()

	// Get nodepool controller status
	controllerStatuses, err := h.repository.Status.ListNodePoolControllerStatus(ctx, id)
	if err != nil {
		h.logger.Error("Failed to get nodepool controller status",
			zap.String("nodepool_id", id.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to get nodepool status",
			err.Error(),
		))
		return
	}

	response := map[string]interface{}{
		"nodepool_id":       id,
		"controller_status": controllerStatuses,
	}

	c.JSON(http.StatusOK, response)
}

// UpdateNodePoolStatus handles controller status updates for nodepools
func (h *NodePoolHandler) UpdateNodePoolStatus(c *gin.Context) {
	idParam := c.Param("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Invalid nodepool ID",
			err.Error(),
		))
		return
	}

	var statusUpdate models.NodePoolControllerStatus
	if err := c.ShouldBindJSON(&statusUpdate); err != nil {
		h.logger.Error("Invalid status update request", zap.Error(err))
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"Invalid status update format",
			err.Error(),
		))
		return
	}

	// Validate required fields
	if statusUpdate.ControllerName == "" {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"controller_name is required",
			"",
		))
		return
	}

	if statusUpdate.Metadata == nil {
		c.JSON(http.StatusBadRequest, utils.NewAPIError(
			utils.ErrCodeValidation,
			"metadata is required",
			"",
		))
		return
	}

	// Set nodepool ID from URL parameter
	statusUpdate.NodePoolID = id

	ctx := c.Request.Context()

	// Get user email from context for client isolation
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		h.logger.Error("No user email found in context")
		c.JSON(http.StatusUnauthorized, utils.NewAPIError(
			utils.ErrCodeUnauthorized,
			"Authentication required",
			"",
		))
		return
	}

	// Verify nodepool exists
	nodepool, err := h.repository.NodePools.GetByID(ctx, id, userEmail)
	if err != nil {
		if err == models.ErrNodePoolNotFound {
			c.JSON(http.StatusNotFound, utils.NewAPIError(
				utils.ErrCodeNotFound,
				"NodePool not found",
				"",
			))
			return
		}

		h.logger.Error("Failed to verify nodepool",
			zap.String("nodepool_id", id.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to verify nodepool",
			err.Error(),
		))
		return
	}

	// Update controller status
	err = h.repository.Status.UpsertNodePoolControllerStatus(ctx, &statusUpdate)
	if err != nil {
		h.logger.Error("Failed to update nodepool controller status",
			zap.String("nodepool_id", id.String()),
			zap.String("controller_name", statusUpdate.ControllerName),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, utils.NewAPIError(
			utils.ErrCodeInternal,
			"Failed to update nodepool status",
			err.Error(),
		))
		return
	}

	// Mark the cluster status as dirty to trigger recalculation on next GET
	if err := h.repository.Clusters.MarkDirtyStatus(ctx, nodepool.ClusterID); err != nil {
		h.logger.Warn("Failed to mark cluster status as dirty",
			zap.String("cluster_id", nodepool.ClusterID.String()),
			zap.Error(err),
		)
		// Don't fail the request for this - status will be recalculated eventually
	}

	// Status update completed - cluster marked as dirty for recalculation
	// No pub/sub events needed in simplified architecture (controllers report via API)

	h.logger.Info("NodePool controller status updated",
		zap.String("nodepool_id", id.String()),
		zap.String("cluster_id", nodepool.ClusterID.String()),
		zap.String("controller_name", statusUpdate.ControllerName),
		zap.Int64("observed_generation", statusUpdate.ObservedGeneration),
	)

	c.JSON(http.StatusOK, map[string]interface{}{
		"message":             "Status updated successfully",
		"nodepool_id":         id.String(),
		"cluster_id":          nodepool.ClusterID.String(),
		"controller_name":     statusUpdate.ControllerName,
		"observed_generation": statusUpdate.ObservedGeneration,
	})
}
