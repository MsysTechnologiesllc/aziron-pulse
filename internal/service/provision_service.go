package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/db"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/k8s"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/models"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/repository"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ProvisionService handles pod provisioning logic
type ProvisionService struct {
	podRepo       *repository.PodRepository
	activityRepo  *repository.ActivityRepository
	quotaRepo     *repository.QuotaRepository
	k8sClient     *k8s.Client
	nsMgr         *k8s.NamespaceManager
	podMgr        *k8s.PodManager
	svcMgr        *k8s.ServiceManager
	volMgr        *k8s.VolumeManager
	logger        *zap.Logger
	workspaceRoot string
}

// ProvisionRequest holds pod provision request data
type ProvisionRequest struct {
	UserID    uuid.UUID
	TenantID  *uuid.UUID
	BaseImage string
	CPULimit  float64
	MemoryMB  int
	StorageGB int
	Metadata  models.JSONBMap
	JWTToken  string // JWT token to use as code-server password
}

// NewProvisionService creates a new provision service
func NewProvisionService(
	database *db.DB,
	k8sClient *k8s.Client,
	workspaceRoot string,
	logger *zap.Logger,
) *ProvisionService {
	return &ProvisionService{
		podRepo:       repository.NewPodRepository(database),
		activityRepo:  repository.NewActivityRepository(database),
		quotaRepo:     repository.NewQuotaRepository(database),
		k8sClient:     k8sClient,
		nsMgr:         k8s.NewNamespaceManager(k8sClient, logger),
		podMgr:        k8s.NewPodManager(k8sClient),
		svcMgr:        k8s.NewServiceManager(k8sClient),
		volMgr:        k8s.NewVolumeManager(k8sClient, logger),
		logger:        logger,
		workspaceRoot: workspaceRoot,
	}
}

// ProvisionPod provisions a new Kubernetes pod
func (s *ProvisionService) ProvisionPod(ctx context.Context, req ProvisionRequest) (*models.PulsePod, error) {
	s.logger.Info("Starting pod provision",
		zap.String("user_id", req.UserID.String()),
		zap.String("base_image", req.BaseImage),
	)

	// Check quota
	quota, err := s.quotaRepo.GetOrCreateDefault(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get quota: %w", err)
	}

	// Validate resource limits
	if req.CPULimit > quota.MaxCPUPerPod {
		return nil, fmt.Errorf("CPU limit %.2f exceeds quota %.2f", req.CPULimit, quota.MaxCPUPerPod)
	}

	if req.MemoryMB > quota.MaxMemoryMBPerPod {
		return nil, fmt.Errorf("memory limit %d exceeds quota %d", req.MemoryMB, quota.MaxMemoryMBPerPod)
	}

	if req.StorageGB > quota.MaxStorageGBPerPod {
		return nil, fmt.Errorf("storage limit %d exceeds quota %d", req.StorageGB, quota.MaxStorageGBPerPod)
	}

	// Check pod count
	activeCount, err := s.podRepo.CountActiveByUserID(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to count active pods: %w", err)
	}

	if activeCount >= quota.MaxPods {
		return nil, fmt.Errorf("pod count %d exceeds quota %d", activeCount, quota.MaxPods)
	}

	// Generate IDs and names
	pulseID := uuid.New().String()
	tenantIDStr := "default"
	if req.TenantID != nil {
		tenantIDStr = req.TenantID.String()
	}

	namespace := k8s.GenerateNamespaceName(tenantIDStr)
	podName := fmt.Sprintf("pulse-%s", strings.Split(pulseID, "-")[0])
	serviceName := fmt.Sprintf("%s-svc", podName)
	pvcName := fmt.Sprintf("%s-pvc", podName)

	// Create workspace path
	workspacePath := filepath.Join(s.workspaceRoot, req.UserID.String(), "pulse", pulseID)

	// Create namespace
	_, err = s.nsMgr.CreateOrGetNamespace(ctx, namespace, map[string]string{
		"app":       "aziron-pulse",
		"tenant-id": tenantIDStr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace: %w", err)
	}

	// Create PVC
	_, err = s.volMgr.CreatePVC(ctx, namespace, pvcName, req.StorageGB, map[string]string{
		"app":      "aziron-pulse",
		"pulse-id": pulseID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PVC: %w", err)
	}

	// Create pod
	_, err = s.podMgr.CreatePod(ctx, k8s.PodConfig{
		Name:          podName,
		Namespace:     namespace,
		Image:         req.BaseImage,
		PVCName:       pvcName,
		WorkspacePath: workspacePath,
		CPULimit:      req.CPULimit,
		MemoryLimitMB: req.MemoryMB,
		Env: map[string]string{
			"JWT_TOKEN": req.JWTToken, // Pass JWT token for code-server password
		},
		Labels: map[string]string{
			"user-id":  req.UserID.String(),
			"pulse-id": pulseID,
		},
	})
	if err != nil {
		// Cleanup on failure
		_ = s.volMgr.DeletePVC(ctx, namespace, pvcName)
		return nil, fmt.Errorf("failed to create pod: %w", err)
	}

	// Create service
	service, err := s.svcMgr.CreateNodePortService(ctx, namespace, serviceName, podName)
	if err != nil {
		// Cleanup on failure
		_ = s.podMgr.DeletePod(ctx, namespace, podName)
		_ = s.volMgr.DeletePVC(ctx, namespace, pvcName)
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	// Get NodePort
	var nodePort *int
	if len(service.Spec.Ports) > 0 {
		port := int(service.Spec.Ports[0].NodePort)
		nodePort = &port
	}

	// Calculate expiry
	now := time.Now()
	ttlMinutes := 120
	expiresAt := now.Add(time.Duration(ttlMinutes) * time.Minute)

	// Extract trace context for span links
	spanCtx := telemetry.ExtractSpanContextForDB(ctx)
	reqCtx := telemetry.GetRequestContext(ctx)

	var traceIDPtr, spanIDPtr, userEmailPtr *string
	if spanCtx != nil {
		traceIDPtr = &spanCtx.TraceID
		spanIDPtr = &spanCtx.SpanID
	}
	if reqCtx.UserEmail != "" {
		userEmailPtr = &reqCtx.UserEmail
	}

	// Create database record
	pod := &models.PulsePod{
		ID:             uuid.New(),
		PulseID:        pulseID,
		UserID:         req.UserID,
		TenantID:       req.TenantID,
		Namespace:      namespace,
		PodName:        podName,
		ServiceName:    serviceName,
		PVCName:        pvcName,
		NodePort:       nodePort,
		Status:         models.PodStatusPending,
		BaseImage:      req.BaseImage,
		CPULimit:       req.CPULimit,
		MemoryLimitMB:  req.MemoryMB,
		StorageGB:      req.StorageGB,
		WorkspacePath:  workspacePath,
		LastActivityAt: now,
		TTLMinutes:     ttlMinutes,
		ExpiresAt:      &expiresAt,
		Metadata:       req.Metadata,
		TraceID:        traceIDPtr,
		SpanID:         spanIDPtr,
		UserEmail:      userEmailPtr,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.podRepo.Create(ctx, pod); err != nil {
		// Cleanup K8s resources on database failure
		_ = s.svcMgr.DeleteService(ctx, namespace, serviceName)
		_ = s.podMgr.DeletePod(ctx, namespace, podName)
		_ = s.volMgr.DeletePVC(ctx, namespace, pvcName)
		return nil, fmt.Errorf("failed to create pod record: %w", err)
	}

	// Log activity
	activity := &models.PodActivity{
		ID:           uuid.New(),
		PodID:        pod.ID,
		ActivityType: models.ActivityTypeCreated,
		Description:  "Pod provisioned successfully",
		Metadata: models.JSONBMap{
			"namespace":  namespace,
			"pod_name":   podName,
			"node_port":  nodePort,
			"base_image": req.BaseImage,
		},
		CreatedAt: now,
	}
	_ = s.activityRepo.Create(ctx, activity)

	s.logger.Info("Pod provisioned successfully",
		zap.String("pulse_id", pulseID),
		zap.String("namespace", namespace),
		zap.String("pod_name", podName),
	)

	return pod, nil
}

// GetPod retrieves a pod by pulse_id
func (s *ProvisionService) GetPod(ctx context.Context, pulseID string) (*models.PulsePod, error) {
	return s.podRepo.GetByPulseID(ctx, pulseID)
}

// ListUserPods lists all pods for a user
func (s *ProvisionService) ListUserPods(ctx context.Context, userID uuid.UUID) ([]*models.PulsePod, error) {
	return s.podRepo.ListByUserID(ctx, userID)
}

// DeletePod deletes a pod and all associated resources
func (s *ProvisionService) DeletePod(ctx context.Context, pulseID string) error {
	pod, err := s.podRepo.GetByPulseID(ctx, pulseID)
	if err != nil {
		return fmt.Errorf("failed to get pod: %w", err)
	}

	// Delete K8s resources
	_ = s.svcMgr.DeleteService(ctx, pod.Namespace, pod.ServiceName)
	_ = s.podMgr.DeletePod(ctx, pod.Namespace, pod.PodName)
	_ = s.volMgr.DeletePVC(ctx, pod.Namespace, pod.PVCName)

	// Soft delete in database
	if err := s.podRepo.SoftDelete(ctx, pod.ID); err != nil {
		return fmt.Errorf("failed to delete pod record: %w", err)
	}

	// Log activity
	activity := &models.PodActivity{
		ID:           uuid.New(),
		PodID:        pod.ID,
		ActivityType: models.ActivityTypeStopped,
		Description:  "Pod deleted",
		CreatedAt:    time.Now(),
	}
	_ = s.activityRepo.Create(ctx, activity)

	s.logger.Info("Pod deleted", zap.String("pulse_id", pulseID))
	return nil
}

// UpdatePodActivity updates the last activity time
func (s *ProvisionService) UpdatePodActivity(ctx context.Context, pulseID string) error {
	pod, err := s.podRepo.GetByPulseID(ctx, pulseID)
	if err != nil {
		return err
	}

	return s.podRepo.UpdateActivity(ctx, pod.ID)
}
