package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PulseMetrics holds all Pulse-specific observability metrics
type PulseMetrics struct {
	// Pod Lifecycle Metrics
	PodProvisionedTotal  *prometheus.CounterVec
	PodLifecycleDuration *prometheus.HistogramVec
	PodActive            *prometheus.GaugeVec

	// Kubernetes API Metrics
	K8sAPIRequestsTotal   *prometheus.CounterVec
	K8sAPIDuration        *prometheus.HistogramVec
	K8sWatchEventsTotal   *prometheus.CounterVec
	K8sWatchReconnections *prometheus.CounterVec

	// Real-time Resource Usage Metrics
	CPUUsageCores         *prometheus.GaugeVec
	MemoryUsageBytes      *prometheus.GaugeVec
	StorageUsedBytes      *prometheus.GaugeVec
	NetworkEgressExternal *prometheus.GaugeVec

	// Quota Metrics
	QuotaUsagePercent  *prometheus.GaugeVec
	QuotaExceededTotal *prometheus.CounterVec

	// Cost Metrics - Resource Level
	CostResourceUSD        *prometheus.CounterVec
	CostPerHourResourceUSD *prometheus.GaugeVec

	// Cost Metrics - Instance Tier
	CostInstanceUSD         *prometheus.CounterVec
	InstanceEquivalentHours *prometheus.CounterVec
	InstanceTierMatched     *prometheus.CounterVec

	// User Activity Metrics
	ActiveSessions     *prometheus.GaugeVec
	ProxyRequestsTotal *prometheus.CounterVec
	IdleDuration       *prometheus.GaugeVec

	// Health Metrics
	PodRestartsTotal *prometheus.CounterVec
	OOMKillsTotal    *prometheus.CounterVec
	TTLCleanupTotal  *prometheus.CounterVec
}

var pulseMetrics *PulseMetrics

// InitPulseMetrics initializes all Pulse-specific metrics
func InitPulseMetrics() *PulseMetrics {
	if pulseMetrics != nil {
		return pulseMetrics
	}

	pulseMetrics = &PulseMetrics{
		// Pod Lifecycle Metrics
		PodProvisionedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_pod_provisioned_total",
				Help: "Total number of pods provisioned",
			},
			[]string{"tenant_id", "user_email", "status", "user_tier"},
		),

		PodLifecycleDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "aziron_pulse_pod_lifecycle_duration_seconds",
				Help:    "Duration of pod lifecycle phases",
				Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600},
			},
			[]string{"phase", "tenant_id", "user_email"},
		),

		PodActive: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aziron_pulse_pod_active_count",
				Help: "Current number of active pods",
			},
			[]string{"tenant_id", "user_email", "namespace"},
		),

		// Kubernetes API Metrics
		K8sAPIRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_k8s_api_requests_total",
				Help: "Total number of Kubernetes API requests",
			},
			[]string{"resource_type", "method", "status_code"},
		),

		K8sAPIDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "aziron_pulse_k8s_api_duration_seconds",
				Help:    "Kubernetes API request duration",
				Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
			},
			[]string{"resource_type", "method"},
		),

		K8sWatchEventsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_k8s_watch_events_total",
				Help: "Total number of Kubernetes watch events",
			},
			[]string{"resource_type", "event_type"},
		),

		K8sWatchReconnections: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_k8s_watch_reconnections_total",
				Help: "Total number of watch reconnections",
			},
			[]string{"reason"},
		),

		// Real-time Resource Usage Metrics
		CPUUsageCores: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aziron_pulse_cpu_usage_cores",
				Help: "Current CPU usage in cores",
			},
			[]string{"tenant_id", "user_email", "namespace"},
		),

		MemoryUsageBytes: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aziron_pulse_memory_usage_bytes",
				Help: "Current memory usage in bytes",
			},
			[]string{"tenant_id", "user_email", "namespace"},
		),

		StorageUsedBytes: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aziron_pulse_storage_used_bytes",
				Help: "Current storage used in bytes",
			},
			[]string{"tenant_id", "user_email", "namespace"},
		),

		NetworkEgressExternal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aziron_pulse_network_egress_external_bytes_total",
				Help: "Total external network egress in bytes (excludes pod-to-pod)",
			},
			[]string{"tenant_id", "user_email", "namespace"},
		),

		// Quota Metrics
		QuotaUsagePercent: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aziron_pulse_quota_usage_percent",
				Help: "Quota usage percentage by resource type",
			},
			[]string{"tenant_id", "user_email", "resource_type"},
		),

		QuotaExceededTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_quota_exceeded_total",
				Help: "Total number of quota exceeded events",
			},
			[]string{"tenant_id", "user_email", "resource_type"},
		),

		// Cost Metrics - Resource Level
		CostResourceUSD: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_cost_resource_usd_total",
				Help: "Total cost in USD by resource type",
			},
			[]string{"tenant_id", "user_email", "resource_type"},
		),

		CostPerHourResourceUSD: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aziron_pulse_cost_per_hour_resource_usd",
				Help: "Current cost per hour in USD by resource type",
			},
			[]string{"tenant_id", "user_email", "resource_type"},
		),

		// Cost Metrics - Instance Tier
		CostInstanceUSD: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_cost_instance_usd_total",
				Help: "Total cost in USD by instance tier",
			},
			[]string{"tenant_id", "user_email", "instance_tier"},
		),

		InstanceEquivalentHours: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_instance_equivalent_hours",
				Help: "Equivalent instance hours consumed",
			},
			[]string{"tenant_id", "user_email", "instance_tier"},
		),

		InstanceTierMatched: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_instance_tier_matched",
				Help: "Instance tier matches (exact vs rounded_up)",
			},
			[]string{"tenant_id", "user_email", "instance_tier", "match_type"},
		),

		// User Activity Metrics
		ActiveSessions: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aziron_pulse_active_sessions",
				Help: "Current number of active user sessions",
			},
			[]string{"tenant_id", "user_email"},
		),

		ProxyRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_proxy_requests_total",
				Help: "Total number of proxy requests to code-server",
			},
			[]string{"tenant_id", "user_email", "status"},
		),

		IdleDuration: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aziron_pulse_idle_duration_seconds",
				Help: "Current idle duration in seconds",
			},
			[]string{"tenant_id", "user_email"},
		),

		// Health Metrics
		PodRestartsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_pod_restarts_total",
				Help: "Total number of pod restarts",
			},
			[]string{"tenant_id", "user_email", "namespace"},
		),

		OOMKillsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_oom_kills_total",
				Help: "Total number of OOM (Out of Memory) kills",
			},
			[]string{"tenant_id", "user_email"},
		),

		TTLCleanupTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aziron_pulse_ttl_cleanup_total",
				Help: "Total number of TTL cleanups",
			},
			[]string{"namespace", "reason"},
		),
	}

	return pulseMetrics
}

// GetPulseMetrics returns the singleton instance of PulseMetrics
func GetPulseMetrics() *PulseMetrics {
	if pulseMetrics == nil {
		return InitPulseMetrics()
	}
	return pulseMetrics
}
