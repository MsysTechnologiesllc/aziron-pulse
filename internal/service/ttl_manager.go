package service

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/db"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/k8s"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/models"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/repository"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
	"github.com/google/uuid"
)

// TTLManager manages pod lifecycle based on TTL
type TTLManager struct {
	podRepo        *repository.PodRepository
	activityRepo   *repository.ActivityRepository
	nsMgr          *k8s.NamespaceManager
	podMgr         *k8s.PodManager
	svcMgr         *k8s.ServiceManager
	volMgr         *k8s.VolumeManager
	logger         *zap.Logger
	interval       time.Duration
	stopCh         chan struct{}
	tracer         trace.Tracer
	costCalculator *telemetry.CostCalculator
}

// NewTTLManager creates a new TTL manager
func NewTTLManager(
	database *db.DB,
	k8sClient *k8s.Client,
	logger *zap.Logger,
	interval time.Duration,
) *TTLManager {
	return &TTLManager{
		podRepo:        repository.NewPodRepository(database),
		activityRepo:   repository.NewActivityRepository(database),
		nsMgr:          k8s.NewNamespaceManager(k8sClient, logger),
		podMgr:         k8s.NewPodManager(k8sClient),
		svcMgr:         k8s.NewServiceManager(k8sClient),
		volMgr:         k8s.NewVolumeManager(k8sClient, logger),
		logger:         logger,
		interval:       interval,
		stopCh:         make(chan struct{}),
		tracer:         otel.Tracer("aziron-pulse/ttl-manager"),
		costCalculator: telemetry.GetCostCalculator(),
	}
}

// Start begins the TTL cleanup loop
func (m *TTLManager) Start(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	m.logger.Info("TTL manager started", zap.Duration("interval", m.interval))

	for {
		select {
		case <-ticker.C:
			if err := m.cleanupExpiredPods(ctx); err != nil {
				m.logger.Error("Failed to cleanup expired pods", zap.Error(err))
			}
		case <-m.stopCh:
			m.logger.Info("TTL manager stopped")
			return
		case <-ctx.Done():
			m.logger.Info("TTL manager context cancelled")
			return
		}
	}
}

// Stop stops the TTL manager
func (m *TTLManager) Stop() {
	close(m.stopCh)
}

func (m *TTLManager) cleanupExpiredPods(ctx context.Context) error {
	pods, err := m.podRepo.GetExpiredPods(ctx)
	if err != nil {
		return err
	}

	if len(pods) == 0 {
		return nil
	}

	m.logger.Info("Found expired pods", zap.Int("count", len(pods)))

	for _, pod := range pods {
		if err := m.cleanupPod(ctx, pod); err != nil {
			m.logger.Error("Failed to cleanup pod",
				zap.String("pulse_id", pod.PulseID),
				zap.Error(err),
			)
			continue
		}

		m.logger.Info("Cleaned up expired pod",
			zap.String("pulse_id", pod.PulseID),
			zap.String("namespace", pod.Namespace),
		)
	}

	return nil
}

func (m *TTLManager) cleanupPod(ctx context.Context, pod *models.PulsePod) error {
	// Create span with link to original provision span if available
	var spanOptions []trace.SpanStartOption
	spanOptions = append(spanOptions, trace.WithAttributes(
		attribute.String("pulse_id", pod.PulseID),
		attribute.String("namespace", pod.Namespace),
		attribute.String("pod_name", pod.PodName),
		attribute.String("cleanup_reason", "ttl_expired"),
	))

	// Create span link to original provision operation
	if pod.TraceID != nil && pod.SpanID != nil && *pod.TraceID != "" && *pod.SpanID != "" {
		if spanCtx, err := telemetry.ParseSpanContext(*pod.TraceID, *pod.SpanID); err == nil {
			spanOptions = append(spanOptions, trace.WithLinks(trace.Link{
				SpanContext: spanCtx,
				Attributes: []attribute.KeyValue{
					attribute.String("link.type", "lifecycle"),
					attribute.String("link.description", "original provision operation"),
				},
			}))
			m.logger.Info("Created span link to provision operation",
				zap.String("provision_trace_id", *pod.TraceID),
				zap.String("provision_span_id", *pod.SpanID),
			)
		}
	}

	ctx, span := m.tracer.Start(ctx, "ttl.cleanup_pod", spanOptions...)
	defer span.End()

	metrics := telemetry.GetPulseMetrics()
	startTime := time.Now()

	// Calculate pod lifetime duration
	podLifetime := time.Since(pod.CreatedAt)
	lifetimeHours := podLifetime.Hours()

	// Calculate resource usage and costs
	usage := telemetry.ResourceUsage{
		CPUCores:    pod.CPULimit,
		MemoryGB:    float64(pod.MemoryLimitMB) / 1024.0,
		StorageGB:   float64(pod.StorageGB),
		NetworkGB:   0, // Will be updated by network collector
		DurationHrs: lifetimeHours,
	}

	costBreakdown := m.costCalculator.CalculateCosts(usage)

	// Emit resource-level cost metrics
	tenantID := "default"
	if pod.TenantID != nil {
		tenantID = pod.TenantID.String()
	}
	userEmail := ""
	if pod.UserEmail != nil {
		userEmail = *pod.UserEmail
	}

	metrics.CostResourceUSD.WithLabelValues(tenantID, userEmail, "cpu").Add(costBreakdown.CPUCost)
	metrics.CostResourceUSD.WithLabelValues(tenantID, userEmail, "memory").Add(costBreakdown.MemoryCost)
	metrics.CostResourceUSD.WithLabelValues(tenantID, userEmail, "storage").Add(costBreakdown.StorageCost)

	// Emit instance tier cost metrics
	if costBreakdown.InstanceTier != "" {
		metrics.CostInstanceUSD.WithLabelValues(tenantID, userEmail, costBreakdown.InstanceTier).Add(costBreakdown.InstanceCost)
		metrics.InstanceEquivalentHours.WithLabelValues(tenantID, userEmail, costBreakdown.InstanceTier).Add(costBreakdown.InstanceHours)
		metrics.InstanceTierMatched.WithLabelValues(tenantID, userEmail, costBreakdown.InstanceTier, costBreakdown.MatchType).Inc()

		span.SetAttributes(
			attribute.String("cost.instance_tier", costBreakdown.InstanceTier),
			attribute.Float64("cost.instance_hours", costBreakdown.InstanceHours),
			attribute.Float64("cost.instance_usd", costBreakdown.InstanceCost),
			attribute.String("cost.match_type", costBreakdown.MatchType),
		)
	}

	span.SetAttributes(
		attribute.Float64("cost.cpu_usd", costBreakdown.CPUCost),
		attribute.Float64("cost.memory_usd", costBreakdown.MemoryCost),
		attribute.Float64("cost.storage_usd", costBreakdown.StorageCost),
		attribute.Float64("cost.total_usd", costBreakdown.TotalResourceCost),
		attribute.Float64("pod.lifetime_hours", lifetimeHours),
	)

	m.logger.Info("Calculated pod costs",
		zap.String("pulse_id", pod.PulseID),
		zap.Float64("lifetime_hours", lifetimeHours),
		zap.Float64("total_resource_cost", costBreakdown.TotalResourceCost),
		zap.String("instance_tier", costBreakdown.InstanceTier),
		zap.Float64("instance_cost", costBreakdown.InstanceCost),
		zap.String("match_type", costBreakdown.MatchType),
	)

	// Update status to expired
	if err := m.podRepo.UpdateStatus(ctx, pod.ID, models.PodStatusExpired); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to update pod status")
		return err
	}

	// Delete K8s resources
	_ = m.svcMgr.DeleteService(ctx, pod.Namespace, pod.ServiceName)
	_ = m.podMgr.DeletePod(ctx, pod.Namespace, pod.PodName)
	_ = m.volMgr.DeletePVC(ctx, pod.Namespace, pod.PVCName)

	// Emit TTL cleanup metrics
	metrics.TTLCleanupTotal.WithLabelValues(pod.Namespace, "ttl_expired").Inc()
	metrics.PodLifecycleDuration.WithLabelValues("ttl_cleanup", tenantID, userEmail).Observe(time.Since(startTime).Seconds())

	// Log activity
	activity := &models.PodActivity{
		ID:           uuid.New(),
		PodID:        pod.ID,
		ActivityType: "expired",
		Description:  "Pod expired via TTL",
		Metadata: models.JSONBMap{
			"lifetime_hours":      lifetimeHours,
			"total_cost_usd":      costBreakdown.TotalResourceCost,
			"instance_tier":       costBreakdown.InstanceTier,
			"instance_cost_usd":   costBreakdown.InstanceCost,
			"instance_match_type": costBreakdown.MatchType,
		},
		CreatedAt: time.Now(),
	}
	_ = m.activityRepo.Create(ctx, activity)

	span.SetStatus(codes.Ok, "Pod cleanup completed successfully")
	return nil
}
