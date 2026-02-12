package k8s

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MsysTechnologiesllc/aziron-pulse/internal/telemetry"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NetworkCollector collects network metrics from cAdvisor
type NetworkCollector struct {
	client           *Client
	namespace        string
	clusterPodCIDR   *net.IPNet
	clusterSvcCIDR   *net.IPNet
	logger           *zap.Logger
	scrapeInterval   time.Duration
	stopCh           chan struct{}
	cadvisorEndpoint string
}

// NetworkMetrics holds parsed network metrics for a pod
type NetworkMetrics struct {
	PodName            string
	Namespace          string
	TenantID           string
	UserEmail          string
	TotalRxBytes       float64
	TotalTxBytes       float64
	ExternalEgressBytes float64
}

// NewNetworkCollector creates a new network metrics collector
func NewNetworkCollector(client *Client, namespace string, logger *zap.Logger) (*NetworkCollector, error) {
	// Parse CLUSTER_POD_CIDR and CLUSTER_SERVICE_CIDR from environment
	podCIDR := getEnv("CLUSTER_POD_CIDR", "10.244.0.0/16")
	svcCIDR := getEnv("CLUSTER_SERVICE_CIDR", "10.96.0.0/12")

	_, clusterPodCIDR, err := net.ParseCIDR(podCIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CLUSTER_POD_CIDR: %w", err)
	}

	_, clusterSvcCIDR, err := net.ParseCIDR(svcCIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CLUSTER_SERVICE_CIDR: %w", err)
	}

	return &NetworkCollector{
		client:           client,
		namespace:        namespace,
		clusterPodCIDR:   clusterPodCIDR,
		clusterSvcCIDR:   clusterSvcCIDR,
		logger:           logger,
		scrapeInterval:   time.Duration(getEnvInt("NETWORK_SCRAPE_INTERVAL", 30)) * time.Second,
		stopCh:           make(chan struct{}),
		cadvisorEndpoint: getEnv("CADVISOR_ENDPOINT", "http://cadvisor.kube-system.svc.cluster.local:8080"),
	}, nil
}

// Start begins the network metrics collection loop
func (c *NetworkCollector) Start(ctx context.Context) error {
	c.logger.Info("Network collector started")

	ticker := time.NewTicker(c.scrapeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			c.logger.Info("Network collector stopped")
			return nil
		case <-ctx.Done():
			c.logger.Info("Network collector context cancelled")
			return nil
		case <-ticker.C:
			if err := c.collectNetworkMetrics(ctx); err != nil {
				c.logger.Error("Failed to collect network metrics")
			}
		}
	}
}

// Stop stops the network collector
func (c *NetworkCollector) Stop() {
	close(c.stopCh)
}

// collectNetworkMetrics scrapes cAdvisor and processes network metrics
func (c *NetworkCollector) collectNetworkMetrics(ctx context.Context) error {
	// List all nodes to scrape cAdvisor from each
	nodes, err := c.client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	for _, node := range nodes.Items {
		if err := c.scrapeNodeCAdvisor(ctx, &node); err != nil {
			c.logger.Error("Failed to scrape cAdvisor from node")
		}
	}

	return nil
}

// scrapeNodeCAdvisor scrapes cAdvisor metrics from a specific node
func (c *NetworkCollector) scrapeNodeCAdvisor(ctx context.Context, node *corev1.Node) error {
	// Build cAdvisor metrics URL for the node
	// In cluster: http://<node-ip>:4194/metrics
	nodeIP := getNodeInternalIP(node)
	if nodeIP == "" {
		return fmt.Errorf("no internal IP found for node %s", node.Name)
	}

	metricsURL := fmt.Sprintf("http://%s:4194/metrics", nodeIP)

	// Scrape metrics
	resp, err := http.Get(metricsURL)
	if err != nil {
		return fmt.Errorf("failed to scrape cAdvisor: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cAdvisor returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse Prometheus metrics
	return c.parseAndEmitNetworkMetrics(ctx, string(body))
}

// parseAndEmitNetworkMetrics parses Prometheus format metrics and emits external egress
func (c *NetworkCollector) parseAndEmitNetworkMetrics(ctx context.Context, metricsText string) error {
	parser := expfmt.TextParser{}
	metricFamilies, err := parser.TextToMetricFamilies(strings.NewReader(metricsText))
	if err != nil {
		return fmt.Errorf("failed to parse metrics: %w", err)
	}

	// Extract container_network_receive_bytes_total and container_network_transmit_bytes_total
	rxFamily, rxOk := metricFamilies["container_network_receive_bytes_total"]
	txFamily, txOk := metricFamilies["container_network_transmit_bytes_total"]

	if !rxOk || !txOk {
		return fmt.Errorf("network metrics not found")
	}

	// Group metrics by pod
	podMetrics := make(map[string]*NetworkMetrics)

	for _, metric := range rxFamily.GetMetric() {
		c.processNetworkMetric(metric, "rx", podMetrics)
	}

	for _, metric := range txFamily.GetMetric() {
		c.processNetworkMetric(metric, "tx", podMetrics)
	}

	// Emit metrics to Prometheus
	metrics := telemetry.GetPulseMetrics()
	for _, netMetrics := range podMetrics {
		// Only track pods in our namespace
		if netMetrics.Namespace != c.namespace {
			continue
		}

		// Emit external egress (we track outbound only)
		metrics.NetworkEgressExternal.WithLabelValues(
			netMetrics.TenantID,
			netMetrics.UserEmail,
			netMetrics.Namespace,
		).Set(netMetrics.ExternalEgressBytes)
	}

	return nil
}

// processNetworkMetric processes a single network metric
func (c *NetworkCollector) processNetworkMetric(metric *dto.Metric, direction string, podMetrics map[string]*NetworkMetrics) {
	labels := metric.GetLabel()
	
	// Extract pod information from labels
	var podName, namespace, containerName string
	for _, label := range labels {
		switch label.GetName() {
		case "pod_name":
			podName = label.GetValue()
		case "namespace":
			namespace = label.GetValue()
		case "name":
			// cAdvisor uses "name" for container
			containerName = label.GetValue()
		}
	}

	// Skip if not a pod container
	if podName == "" || namespace == "" {
		return
	}

	// Skip non-application containers (pause, init)
	if containerName == "POD" || strings.HasPrefix(containerName, "k8s_") {
		return
	}

	key := fmt.Sprintf("%s/%s", namespace, podName)
	if _, exists := podMetrics[key]; !exists {
		podMetrics[key] = &NetworkMetrics{
			PodName:   podName,
			Namespace: namespace,
		}
	}

	value := metric.GetCounter().GetValue()

	if direction == "rx" {
		podMetrics[key].TotalRxBytes += value
	} else {
		podMetrics[key].TotalTxBytes += value
		// TX is egress - filter for external traffic
		podMetrics[key].ExternalEgressBytes += c.calculateExternalEgress(value, labels)
	}
}

// calculateExternalEgress determines if traffic is external (outside cluster)
func (c *NetworkCollector) calculateExternalEgress(txBytes float64, labels []*dto.LabelPair) float64 {
	// Extract destination IP from interface labels if available
	// cAdvisor doesn't provide per-destination metrics directly,
	// so we estimate external egress as total egress minus internal patterns

	// For now, use a heuristic: assume 20% of egress is external
	// In production, you'd use eBPF or network policies to get accurate data
	externalRatio := getEnvFloat("EXTERNAL_EGRESS_RATIO", 0.20)
	return txBytes * externalRatio
}

// isInternalIP checks if an IP is internal to the cluster
func (c *NetworkCollector) isInternalIP(ip net.IP) bool {
	// Check if IP is in pod CIDR
	if c.clusterPodCIDR.Contains(ip) {
		return true
	}

	// Check if IP is in service CIDR
	if c.clusterSvcCIDR.Contains(ip) {
		return true
	}

	// Check if IP is private (RFC1918)
	_, private10, _ := net.ParseCIDR("10.0.0.0/8")
	_, private172, _ := net.ParseCIDR("172.16.0.0/12")
	_, private192, _ := net.ParseCIDR("192.168.0.0/16")

	if private10.Contains(ip) || private172.Contains(ip) || private192.Contains(ip) {
		return true
	}

	return false
}

// getNodeInternalIP extracts the internal IP of a node
func getNodeInternalIP(node *corev1.Node) string {
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address
		}
	}
	return ""
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value, exists := os.LookupEnv(key); exists {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}
