package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/logging"
	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// MetricsWatcher watches pod metrics and emits Prometheus gauges
type MetricsWatcher struct {
	clientset        *kubernetes.Clientset
	metricsClientset *metricsv.Clientset
	namespace        string
	resourceVersion  string
	mu               sync.RWMutex
	
	// In-memory cache of pod metadata for label enrichment
	podMetadata map[string]*PodMetadata
	metadataMu  sync.RWMutex
	
	// Flush interval for persisting resourceVersion
	flushInterval time.Duration
	stopCh        chan struct{}
}

// PodMetadata holds enrichment data for metrics
type PodMetadata struct {
	PodName    string
	Namespace  string
	TenantID   string
	UserEmail  string
	PulseID    string
	UpdatedAt  time.Time
}

// NewMetricsWatcher creates a new metrics watcher
func NewMetricsWatcher(clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, namespace string) *MetricsWatcher {
	return &MetricsWatcher{
		clientset:        clientset,
		metricsClientset: metricsClientset,
		namespace:        namespace,
		podMetadata:      make(map[string]*PodMetadata),
		flushInterval:    1 * time.Minute,
		stopCh:           make(chan struct{}),
	}
}

// Start begins watching pod events and collecting metrics
func (w *MetricsWatcher) Start(ctx context.Context) error {
	logger := logging.FromContext(ctx)
	
	// Start pod metadata watcher
	go w.watchPodMetadata(ctx)
	
	// Start metrics collection loop
	go w.collectMetrics(ctx)
	
	// Start resourceVersion flush loop
	go w.flushResourceVersion(ctx)
	
	logger.Info("Metrics watcher started")
	
	return nil
}

// Stop gracefully stops the watcher
func (w *MetricsWatcher) Stop() {
	close(w.stopCh)
}

// watchPodMetadata watches pod events to maintain metadata cache
func (w *MetricsWatcher) watchPodMetadata(ctx context.Context) {
	logger := logging.FromContext(ctx)
	
	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		
		// Get current resourceVersion
		w.mu.RLock()
		resourceVersion := w.resourceVersion
		w.mu.RUnlock()
		
		// Create watch with bookmarks enabled
		watcher, err := w.clientset.CoreV1().Pods(w.namespace).Watch(ctx, metav1.ListOptions{
			ResourceVersion:   resourceVersion,
			AllowWatchBookmarks: true,
		})
		
		if err != nil {
			logger.Error("Failed to create pod watcher")
			time.Sleep(5 * time.Second)
			continue
		}
		
		w.handlePodEvents(ctx, watcher)
		watcher.Stop()
		
		// Brief pause before reconnecting
		time.Sleep(1 * time.Second)
	}
}

// handlePodEvents processes pod watch events
func (w *MetricsWatcher) handlePodEvents(ctx context.Context, watcher watch.Interface) {
	logger := logging.FromContext(ctx)
	
	for event := range watcher.ResultChan() {
		switch event.Type {
		case watch.Added, watch.Modified:
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			
			w.updatePodMetadata(pod)
			
			// Update resourceVersion
			w.mu.Lock()
			w.resourceVersion = pod.ResourceVersion
			w.mu.Unlock()
			
		case watch.Deleted:
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			
			w.deletePodMetadata(pod.Name)
			
			// Update resourceVersion
			w.mu.Lock()
			w.resourceVersion = pod.ResourceVersion
			w.mu.Unlock()
			
		case watch.Bookmark:
			// Bookmark events allow us to keep resourceVersion up-to-date
			// without processing events
			if event.Object != nil {
				if pod, ok := event.Object.(*corev1.Pod); ok {
					w.mu.Lock()
					w.resourceVersion = pod.ResourceVersion
					w.mu.Unlock()
				}
			}
			
		case watch.Error:
			logger.Error("Watch error event received")
		}
	}
}

// updatePodMetadata updates the metadata cache for a pod
func (w *MetricsWatcher) updatePodMetadata(pod *corev1.Pod) {
	// Extract metadata from pod labels/annotations
	metadata := &PodMetadata{
		PodName:   pod.Name,
		Namespace: pod.Namespace,
		UpdatedAt: time.Now(),
	}
	
	// Extract tenant_id from labels
	if tenantID, ok := pod.Labels["tenant_id"]; ok {
		metadata.TenantID = tenantID
	}
	
	// Extract user_email from annotations
	if userEmail, ok := pod.Annotations["user_email"]; ok {
		metadata.UserEmail = userEmail
	}
	
	// Extract pulse_id from labels
	if pulseID, ok := pod.Labels["pulse_id"]; ok {
		metadata.PulseID = pulseID
	}
	
	w.metadataMu.Lock()
	w.podMetadata[pod.Name] = metadata
	w.metadataMu.Unlock()
}

// deletePodMetadata removes metadata from cache
func (w *MetricsWatcher) deletePodMetadata(podName string) {
	w.metadataMu.Lock()
	delete(w.podMetadata, podName)
	w.metadataMu.Unlock()
}

// getPodMetadata retrieves cached metadata
func (w *MetricsWatcher) getPodMetadata(podName string) *PodMetadata {
	w.metadataMu.RLock()
	defer w.metadataMu.RUnlock()
	return w.podMetadata[podName]
}

// collectMetrics periodically collects and emits pod metrics
func (w *MetricsWatcher) collectMetrics(ctx context.Context) {
	logger := logging.FromContext(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.scrapeAndEmitMetrics(ctx); err != nil {
				logger.Error("Failed to collect metrics")
			}
		}
	}
}

// scrapeAndEmitMetrics scrapes metrics from K8s metrics API and emits to Prometheus
func (w *MetricsWatcher) scrapeAndEmitMetrics(ctx context.Context) error {
	logger := logging.FromContext(ctx)
	
	// Get pod metrics from metrics-server
	podMetricsList, err := w.metricsClientset.MetricsV1beta1().PodMetricses(w.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pod metrics: %w", err)
	}
	
	// Process each pod's metrics
	for _, podMetrics := range podMetricsList.Items {
		w.emitPodMetrics(ctx, &podMetrics)
	}
	
	logger.Debug("Collected metrics for pods")
	
	return nil
}

// emitPodMetrics emits Prometheus metrics for a single pod
func (w *MetricsWatcher) emitPodMetrics(ctx context.Context, podMetrics *v1beta1.PodMetrics) {
	// Get cached metadata
	metadata := w.getPodMetadata(podMetrics.Name)
	if metadata == nil {
		// Skip if we don't have metadata yet
		return
	}
	
	// Aggregate container metrics
	var totalCPU, totalMemory int64
	for _, container := range podMetrics.Containers {
		// CPU in millicores
		cpuMillis := container.Usage.Cpu().MilliValue()
		totalCPU += cpuMillis
		
		// Memory in bytes
		memoryBytes := container.Usage.Memory().Value()
		totalMemory += memoryBytes
	}
	
	// Get metrics instance
	metrics := telemetry.GetPulseMetrics()
	
	// Emit CPU usage (convert millicores to cores)
	cpuCores := float64(totalCPU) / 1000.0
	metrics.CPUUsageCores.WithLabelValues(
		metadata.TenantID,
		metadata.UserEmail,
		metadata.Namespace,
	).Set(cpuCores)
	
	// Emit memory usage (in bytes)
	metrics.MemoryUsageBytes.WithLabelValues(
		metadata.TenantID,
		metadata.UserEmail,
		metadata.Namespace,
	).Set(float64(totalMemory))
}

// flushResourceVersion periodically persists resourceVersion to database
func (w *MetricsWatcher) flushResourceVersion(ctx context.Context) {
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-w.stopCh:
			// Final flush before stopping
			w.persistResourceVersion(ctx)
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.persistResourceVersion(ctx)
		}
	}
}

// persistResourceVersion saves the current resourceVersion
// In a real implementation, this would write to database
// For now, we'll just log it
func (w *MetricsWatcher) persistResourceVersion(ctx context.Context) {
	w.mu.RLock()
	rv := w.resourceVersion
	w.mu.RUnlock()
	
	if rv == "" {
		return
	}
	
	logger := logging.FromContext(ctx)
	logger.Debug("Persisting resourceVersion")
	
	// TODO: Persist to database
	// For now, storing in memory is sufficient as Watch API
	// will handle reconnects gracefully
}

// GetStats returns current watcher statistics
func (w *MetricsWatcher) GetStats() map[string]interface{} {
	w.mu.RLock()
	rv := w.resourceVersion
	w.mu.RUnlock()
	
	w.metadataMu.RLock()
	podCount := len(w.podMetadata)
	w.metadataMu.RUnlock()
	
	return map[string]interface{}{
		"resource_version":   rv,
		"cached_pods":        podCount,
		"namespace":          w.namespace,
		"flush_interval_sec": w.flushInterval.Seconds(),
	}
}

// GetStatsJSON returns statistics as JSON string
func (w *MetricsWatcher) GetStatsJSON() string {
	stats := w.GetStats()
	data, _ := json.Marshal(stats)
	return string(data)
}
