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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
)

// PodManager manages Kubernetes pods
type PodManager struct {
	client *Client
	logger *zap.Logger
	tracer trace.Tracer
}

// NewPodManager creates a new pod manager
func NewPodManager(client *Client) *PodManager {
	return &PodManager{
		client: client,
		logger: client.Logger,
		tracer: otel.Tracer("aziron-pulse/k8s/pod"),
	}
}

// PodConfig holds configuration for creating a pod
type PodConfig struct {
	Name          string
	Namespace     string
	Image         string
	PVCName       string
	WorkspacePath string
	CPULimit      float64
	MemoryLimitMB int
	Labels        map[string]string
	Env           map[string]string
}

// CreatePod creates a new pod with code-server
func (m *PodManager) CreatePod(ctx context.Context, config PodConfig) (*corev1.Pod, error) {
	ctx, span := m.tracer.Start(ctx, "k8s.create_pod",
		trace.WithAttributes(
			attribute.String("k8s.namespace", config.Namespace),
			attribute.String("k8s.pod.name", config.Name),
			attribute.Float64("k8s.resource.cpu", config.CPULimit),
			attribute.Int("k8s.resource.memory_mb", config.MemoryLimitMB),
			attribute.String("k8s.image", config.Image),
		))
	defer span.End()

	start := time.Now()
	metrics := telemetry.GetPulseMetrics()

	// Extract user context for metrics
	reqCtx := telemetry.GetRequestContext(ctx)

	cpuLimit := fmt.Sprintf("%.2f", config.CPULimit)
	memoryLimit := fmt.Sprintf("%dMi", config.MemoryLimitMB)

	// Set default labels
	if config.Labels == nil {
		config.Labels = make(map[string]string)
	}
	config.Labels["app"] = "aziron-pulse"
	config.Labels["pulse-pod"] = config.Name

	// Build environment variables
	// Use JWT_TOKEN if provided, otherwise fallback to default
	password := "aziron"
	if jwtToken, ok := config.Env["JWT_TOKEN"]; ok && jwtToken != "" {
		password = jwtToken
	}
	
	envVars := []corev1.EnvVar{
		{Name: "PASSWORD", Value: password},
		{Name: "SUDO_PASSWORD", Value: "aziron"}, // Keep sudo password as default
	}
	for key, value := range config.Env {
		// Skip JWT_TOKEN as it's already used for PASSWORD
		if key == "JWT_TOKEN" {
			continue
		}
		envVars = append(envVars, corev1.EnvVar{Name: key, Value: value})
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
			Labels:    config.Labels,
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:    int64Ptr(1000),
				RunAsNonRoot: boolPtr(true),
				FSGroup:      int64Ptr(1000),
			},
			Containers: []corev1.Container{
				{
					Name:  "code-server",
					Image: config.Image,
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 8080,
							Protocol:      corev1.ProtocolTCP,
						},
					},
					Env: envVars,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpuLimit),
							corev1.ResourceMemory: resource.MustParse(memoryLimit),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "workspace",
							MountPath: "/home/coder/workspace",
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/healthz",
								Port: intstr.FromInt(8080),
							},
						},
						InitialDelaySeconds: 30,
						PeriodSeconds:       10,
						TimeoutSeconds:      5,
						FailureThreshold:    3,
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/healthz",
								Port: intstr.FromInt(8080),
							},
						},
						InitialDelaySeconds: 10,
						PeriodSeconds:       5,
						TimeoutSeconds:      3,
						FailureThreshold:    3,
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "workspace",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: config.PVCName,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
		},
	}

	createdPod, err := m.client.Clientset.CoreV1().Pods(config.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existing, getErr := m.client.Clientset.CoreV1().Pods(config.Namespace).Get(ctx, config.Name, metav1.GetOptions{})
			if getErr == nil {
				// Emit metrics for existing pod
				metrics.K8sAPIRequestsTotal.WithLabelValues("pod", "create", "409").Inc()
				metrics.K8sAPIDuration.WithLabelValues("pod", "create").Observe(time.Since(start).Seconds())
			}
			return existing, getErr
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		metrics.K8sAPIRequestsTotal.WithLabelValues("pod", "create", "500").Inc()
		metrics.K8sAPIDuration.WithLabelValues("pod", "create").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("failed to create pod: %w", err)
	}

	// Emit success metrics
	metrics.K8sAPIRequestsTotal.WithLabelValues("pod", "create", "201").Inc()
	metrics.K8sAPIDuration.WithLabelValues("pod", "create").Observe(time.Since(start).Seconds())
	metrics.PodProvisionedTotal.WithLabelValues(reqCtx.TenantID, reqCtx.UserEmail, "success", "").Inc()
	metrics.PodLifecycleDuration.WithLabelValues("provision", reqCtx.TenantID, reqCtx.UserEmail).Observe(time.Since(start).Seconds())

	span.SetStatus(codes.Ok, "Pod created successfully")

	m.logger.Info("Created pod",
		zap.String("namespace", config.Namespace),
		zap.String("name", config.Name),
		zap.String("image", config.Image),
		zap.String("trace_id", span.SpanContext().TraceID().String()),
		zap.String("span_id", span.SpanContext().SpanID().String()),
	)

	return createdPod, nil
}

// GetPod retrieves a pod
func (m *PodManager) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	ctx, span := m.tracer.Start(ctx, "k8s.get_pod",
		trace.WithAttributes(
			attribute.String("k8s.namespace", namespace),
			attribute.String("k8s.pod.name", name),
		))
	defer span.End()

	start := time.Now()
	metrics := telemetry.GetPulseMetrics()

	pod, err := m.client.Clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		statusCode := "500"
		if errors.IsNotFound(err) {
			statusCode = "404"
		}
		metrics.K8sAPIRequestsTotal.WithLabelValues("pod", "get", statusCode).Inc()
		metrics.K8sAPIDuration.WithLabelValues("pod", "get").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	metrics.K8sAPIRequestsTotal.WithLabelValues("pod", "get", "200").Inc()
	metrics.K8sAPIDuration.WithLabelValues("pod", "get").Observe(time.Since(start).Seconds())
	span.SetStatus(codes.Ok, "Pod retrieved successfully")

	return pod, nil
}

// DeletePod deletes a pod
func (m *PodManager) DeletePod(ctx context.Context, namespace, name string) error {
	ctx, span := m.tracer.Start(ctx, "k8s.delete_pod",
		trace.WithAttributes(
			attribute.String("k8s.namespace", namespace),
			attribute.String("k8s.pod.name", name),
		))
	defer span.End()

	start := time.Now()
	metrics := telemetry.GetPulseMetrics()
	reqCtx := telemetry.GetRequestContext(ctx)

	err := m.client.Clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		metrics.K8sAPIRequestsTotal.WithLabelValues("pod", "delete", "500").Inc()
		metrics.K8sAPIDuration.WithLabelValues("pod", "delete").Observe(time.Since(start).Seconds())
		return fmt.Errorf("failed to delete pod: %w", err)
	}

	statusCode := "200"
	if errors.IsNotFound(err) {
		statusCode = "404"
	}
	metrics.K8sAPIRequestsTotal.WithLabelValues("pod", "delete", statusCode).Inc()
	metrics.K8sAPIDuration.WithLabelValues("pod", "delete").Observe(time.Since(start).Seconds())
	metrics.PodLifecycleDuration.WithLabelValues("cleanup", reqCtx.TenantID, reqCtx.UserEmail).Observe(time.Since(start).Seconds())

	span.SetStatus(codes.Ok, "Pod deleted successfully")

	m.logger.Info("Deleted pod",
		zap.String("namespace", namespace),
		zap.String("name", name),
		zap.String("trace_id", span.SpanContext().TraceID().String()),
	)
	return nil
}

// ListPods lists all pods in a namespace with optional label selector
func (m *PodManager) ListPods(ctx context.Context, namespace string, labelSelector string) (*corev1.PodList, error) {
	pods, err := m.client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	return pods, nil
}

// GetPodStatus gets the status of a pod
func (m *PodManager) GetPodStatus(ctx context.Context, namespace, name string) (string, error) {
	pod, err := m.GetPod(ctx, namespace, name)
	if err != nil {
		return "", err
	}

	// Check pod phase
	switch pod.Status.Phase {
	case corev1.PodRunning:
		// Check if all containers are ready
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return "running", nil
			}
		}
		return "starting", nil
	case corev1.PodPending:
		return "pending", nil
	case corev1.PodSucceeded:
		return "terminated", nil
	case corev1.PodFailed:
		return "failed", nil
	default:
		return "unknown", nil
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}
