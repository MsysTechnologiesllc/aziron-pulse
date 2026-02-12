package k8s

import (
	"context"
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
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
)

// ServiceManager manages Kubernetes services
type ServiceManager struct {
	client *Client
	logger *zap.Logger
	tracer trace.Tracer
}

// NewServiceManager creates a new service manager
func NewServiceManager(client *Client) *ServiceManager {
	return &ServiceManager{
		client: client,
		logger: client.Logger,
		tracer: otel.Tracer("aziron-pulse/k8s/service"),
	}
}

// CreateNodePortService creates a NodePort service for a pod
func (m *ServiceManager) CreateNodePortService(ctx context.Context, namespace, name, podName string) (*corev1.Service, error) {
	ctx, span := m.tracer.Start(ctx, "k8s.create_service",
		trace.WithAttributes(
			attribute.String("k8s.namespace", namespace),
			attribute.String("k8s.service.name", name),
			attribute.String("k8s.service.type", "NodePort"),
		))
	defer span.End()

	start := time.Now()
	metrics := telemetry.GetPulseMetrics()

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "aziron-pulse",
				"pulse-pod": podName,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Selector: map[string]string{
				"pulse-pod": podName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}

	createdService, err := m.client.Clientset.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			metrics.K8sAPIRequestsTotal.WithLabelValues("service", "create", "409").Inc()
			return m.client.Clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		metrics.K8sAPIRequestsTotal.WithLabelValues("service", "create", "500").Inc()
		metrics.K8sAPIDuration.WithLabelValues("service", "create").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	metrics.K8sAPIRequestsTotal.WithLabelValues("service", "create", "201").Inc()
	metrics.K8sAPIDuration.WithLabelValues("service", "create").Observe(time.Since(start).Seconds())
	span.SetStatus(codes.Ok, "Service created successfully")

	m.logger.Info("Created NodePort service",
		zap.String("namespace", namespace),
		zap.String("name", name),
		zap.Int32("nodePort", createdService.Spec.Ports[0].NodePort),
	)

	return createdService, nil
}

// GetService retrieves a service
func (m *ServiceManager) GetService(ctx context.Context, namespace, name string) (*corev1.Service, error) {
	service, err := m.client.Clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}
	return service, nil
}

// DeleteService deletes a service
func (m *ServiceManager) DeleteService(ctx context.Context, namespace, name string) error {
	err := m.client.Clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	m.logger.Info("Deleted service", zap.String("namespace", namespace), zap.String("name", name))
	return nil
}

// GetNodePort gets the NodePort from a service
func (m *ServiceManager) GetNodePort(ctx context.Context, namespace, name string) (int32, error) {
	service, err := m.GetService(ctx, namespace, name)
	if err != nil {
		return 0, err
	}

	if len(service.Spec.Ports) == 0 {
		return 0, fmt.Errorf("service has no ports")
	}

	return service.Spec.Ports[0].NodePort, nil
}
