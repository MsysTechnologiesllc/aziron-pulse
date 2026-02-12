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

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
)

// VolumeManager handles Kubernetes volume operations
type VolumeManager struct {
	client *Client
	logger *zap.Logger
	tracer trace.Tracer
}

// NewVolumeManager creates a new volume manager
func NewVolumeManager(client *Client, logger *zap.Logger) *VolumeManager {
	return &VolumeManager{
		client: client,
		logger: logger,
		tracer: otel.Tracer("aziron-pulse/k8s/volume"),
	}
}

// CreatePVC creates a persistent volume claim
func (m *VolumeManager) CreatePVC(ctx context.Context, namespace, name string, storageGB int, labels map[string]string) (*corev1.PersistentVolumeClaim, error) {
	ctx, span := m.tracer.Start(ctx, "k8s.create_pvc",
		trace.WithAttributes(
			attribute.String("k8s.namespace", namespace),
			attribute.String("k8s.pvc.name", name),
			attribute.Int("k8s.storage.size_gb", storageGB),
		))
	defer span.End()

	start := time.Now()
	metrics := telemetry.GetPulseMetrics()

	storageClassName := "manual"
	storageSize := fmt.Sprintf("%dGi", storageGB)

	// Create PV first (hostPath for local development)
	hostPath := fmt.Sprintf("/tmp/pulse-volumes/%s", name)
	if err := m.createHostPathPV(ctx, name, storageGB, hostPath); err != nil {
		return nil, fmt.Errorf("failed to create PV: %w", err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
			StorageClassName: &storageClassName,
		},
	}

	// Bind to specific PV
	pvc.Spec.VolumeName = name

	createdPVC, err := m.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			metrics.K8sAPIRequestsTotal.WithLabelValues("pvc", "create", "409").Inc()
			return m.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		metrics.K8sAPIRequestsTotal.WithLabelValues("pvc", "create", "500").Inc()
		metrics.K8sAPIDuration.WithLabelValues("pvc", "create").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("failed to create PVC: %w", err)
	}

	metrics.K8sAPIRequestsTotal.WithLabelValues("pvc", "create", "201").Inc()
	metrics.K8sAPIDuration.WithLabelValues("pvc", "create").Observe(time.Since(start).Seconds())
	span.SetStatus(codes.Ok, "PVC created successfully")

	m.logger.Info("Created PVC",
		zap.String("namespace", namespace),
		zap.String("name", name),
		zap.String("size", storageSize),
	)

	return createdPVC, nil
}

// createHostPathPV creates a hostPath persistent volume
func (m *VolumeManager) createHostPathPV(ctx context.Context, name string, storageGB int, hostPath string) error {
	storageSize := fmt.Sprintf("%dGi", storageGB)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app":     "aziron-pulse",
				"manager": "pulse",
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(storageSize),
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              "manual",
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostPath,
					Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
	}

	_, err := m.client.Clientset.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create PV: %w", err)
	}

	m.logger.Info("Created PV",
		zap.String("name", name),
		zap.String("path", hostPath),
		zap.String("size", storageSize),
	)

	return nil
}

// DeletePVC deletes a persistent volume claim
func (m *VolumeManager) DeletePVC(ctx context.Context, namespace, name string) error {
	err := m.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete PVC: %w", err)
	}

	// Also delete the PV if it exists
	_ = m.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, name, metav1.DeleteOptions{})

	m.logger.Info("Deleted PVC", zap.String("namespace", namespace), zap.String("name", name))
	return nil
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func hostPathTypePtr(t corev1.HostPathType) *corev1.HostPathType {
	return &t
}
