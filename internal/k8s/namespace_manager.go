package k8s

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
)

// NamespaceManager handles Kubernetes namespace operations
type NamespaceManager struct {
	client *Client
	logger *zap.Logger
	tracer trace.Tracer
}

// NewNamespaceManager creates a new namespace manager
func NewNamespaceManager(client *Client, logger *zap.Logger) *NamespaceManager {
	return &NamespaceManager{
		client: client,
		logger: logger,
		tracer: otel.Tracer("aziron-pulse/k8s/namespace"),
	}
}

// GenerateNamespaceName creates a unique namespace name for a tenant
func GenerateNamespaceName(tenantID string) string {
	// Create a hash of the tenant ID for consistent naming
	hash := sha256.Sum256([]byte(tenantID))
	return fmt.Sprintf("pulse-tenant-%x", hash[:8])
}

// CreateOrGetNamespace creates a namespace or returns existing one
func (m *NamespaceManager) CreateOrGetNamespace(ctx context.Context, name string, labels map[string]string) (*corev1.Namespace, error) {
	ctx, span := m.tracer.Start(ctx, "k8s.create_or_get_namespace",
		trace.WithAttributes(
			attribute.String("k8s.namespace", name),
		))
	defer span.End()

	start := time.Now()
	metrics := telemetry.GetPulseMetrics()

	// Try to get existing namespace
	ns, err := m.client.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		metrics.K8sAPIRequestsTotal.WithLabelValues("namespace", "get", "200").Inc()
		metrics.K8sAPIDuration.WithLabelValues("namespace", "get").Observe(time.Since(start).Seconds())
		span.SetStatus(codes.Ok, "Namespace already exists")
		m.logger.Info("Namespace already exists", zap.String("namespace", name))
		return ns, nil
	}

	// Create if doesn't exist
	if !errors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get namespace: %w", err)
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}

	ns, err = m.client.Clientset.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if err != nil {
		// Race condition - namespace was created between get and create
		if errors.IsAlreadyExists(err) {
			metrics.K8sAPIRequestsTotal.WithLabelValues("namespace", "create", "409").Inc()
			return m.client.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		metrics.K8sAPIRequestsTotal.WithLabelValues("namespace", "create", "500").Inc()
		metrics.K8sAPIDuration.WithLabelValues("namespace", "create").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("failed to create namespace: %w", err)
	}

	metrics.K8sAPIRequestsTotal.WithLabelValues("namespace", "create", "201").Inc()
	metrics.K8sAPIDuration.WithLabelValues("namespace", "create").Observe(time.Since(start).Seconds())
	span.SetStatus(codes.Ok, "Namespace created successfully")

	m.logger.Info("Created namespace", zap.String("namespace", name))
	return ns, nil
}

// DeleteNamespace deletes a namespace
func (m *NamespaceManager) DeleteNamespace(ctx context.Context, name string) error {
	err := m.client.Clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete namespace: %w", err)
	}

	m.logger.Info("Deleted namespace", zap.String("namespace", name))
	return nil
}

// ListNamespaces lists all pulse-related namespaces
func (m *NamespaceManager) ListNamespaces(ctx context.Context) (*corev1.NamespaceList, error) {
	labelSelector := "app=aziron-pulse"
	namespaces, err := m.client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	return namespaces, nil
}
