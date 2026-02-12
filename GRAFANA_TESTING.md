# Grafana Dashboard Testing Guide

## Prerequisites

1. **Start Local Telemetry Stack** (Tempo, Prometheus, Grafana)
   ```bash
   cd /Users/damirdarasu/workspace/aziron-telemetry/app-server
   docker-compose -f docker-compose.monitoring.yml up -d
   ```

2. **Deploy cAdvisor DaemonSet** (for network metrics)
   ```bash
   kubectl apply -f deploy/k8s/cadvisor-daemonset.yaml
   ```

3. **Set Environment Variables**
   ```bash
   export PULSE_PORT=8081
   export DB_HOST=localhost
   export DB_PORT=5432
   export DB_USER=aziron
   export DB_PASSWORD=aziron123
   export DB_NAME=aziron
   export DB_SSLMODE=disable
   export JWT_SECRET=your-secret-key-change-in-production
   export KUBECONFIG=/Users/damirdarasu/.kube/config
   export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
   export OTEL_SERVICE_NAME=aziron-pulse
   export LOG_LEVEL=info
   export K8S_NAMESPACE=aziron-pulse
   export CLUSTER_POD_CIDR=10.244.0.0/16
   export CLUSTER_SERVICE_CIDR=10.96.0.0/12
   export NETWORK_SCRAPE_INTERVAL=30s
   ```

## Run Aziron Pulse

```bash
cd /Users/damirdarasu/workspace/Aziron/aziron-pulse
./bin/aziron-pulse
```

## Generate Test Metrics

### 1. **Provision Pods** (generates lifecycle + resource metrics)
```bash
curl -X POST http://localhost:8081/provision \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "user_email": "test@example.com",
    "tenant_id": "test-tenant",
    "instance_tier": "balanced",
    "cpu_limit": "2.0",
    "memory_mb": "4096",
    "storage_gb": "10",
    "ttl_hours": "2"
  }'
```

### 2. **Check Metrics Endpoint**
```bash
curl http://localhost:8081/metrics | grep aziron_pulse
```

**Expected Metrics:**
- `aziron_pulse_cpu_usage_cores` (from metrics watcher)
- `aziron_pulse_memory_usage_bytes` (from metrics watcher)
- `aziron_pulse_network_egress_external_bytes_total` (from network collector)
- `aziron_pulse_pod_provisioned_total`
- `aziron_pulse_cost_resource_usd_total`
- `aziron_pulse_cost_instance_usd_total`
- `aziron_pulse_k8s_api_requests_total`
- `aziron_pulse_quota_usage_percent`

### 3. **Verify Prometheus Scraping**
```bash
# Check if Prometheus is scraping aziron-pulse
curl http://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | select(.labels.job == "aziron-pulse")'
```

## Import Grafana Dashboard

1. **Access Grafana**: http://localhost:3000 (default: admin/admin)

2. **Import Dashboard**:
   - Navigate to **Dashboards** → **New** → **Import**
   - Upload: `/Users/damirdarasu/workspace/aziron-telemetry/configs/grafana/dashboards/aziron-pulse-operations.json`
   - Select Prometheus datasource: `Prometheus`
   - Select Tempo datasource: `Tempo`
   - Click **Import**

3. **Configure Data Sources** (if not already set up):
   - **Prometheus**: http://prometheus:9090
   - **Tempo**: http://tempo:3200

## Dashboard Panels to Verify

### **User Resource Overview Row**
- ✅ **CPU Usage (Cores)**: Should show real-time CPU usage from K8s metrics-server
- ✅ **Memory Usage**: Should show real-time memory usage
- ✅ **External Network Egress Rate**: Should show network traffic from cAdvisor (after ~30s)

### **Cost Tracking Row**
- ✅ **Resource-Level Cost Breakdown**: Pie chart with CPU/Memory/Storage/Network costs
- ✅ **Instance Tier Cost Distribution**: Pie chart showing tier usage (balanced, burstable, etc.)

### **Pod Lifecycle & Activity Row**
- ✅ **Active Pods**: Stat panel showing current active pod count
- ✅ **Provision Success Rate**: Should be close to 100%
- ✅ **TTL Cleanup Traces**: Click to view traces with span links to provision operations

### **Kubernetes API Health Row**
- ✅ **K8s API Request Rate**: Stacked area chart by resource_type and method
- ✅ **K8s API Latency**: Line chart with p50/p95/p99 percentiles

## Test Scenarios

### Scenario 1: User Resource Monitoring
1. Provision 2-3 pods with different user_email values
2. Select specific `user_email` in dashboard dropdown
3. Verify CPU/Memory/Network metrics show only that user's data

### Scenario 2: Cost Tracking
1. Provision pods with different instance tiers (burstable, balanced, compute_optimized)
2. Wait 2-3 minutes for cost metrics to accumulate
3. Verify both pie charts show cost distribution

### Scenario 3: Span Links & Traces
1. Wait for TTL cleanup to trigger (check logs for "TTL cleanup")
2. Click on "TTL Cleanup Traces" panel
3. Click a trace → Verify span links to original provision operation
4. Confirm dual cost tracking in span attributes (resource_cost_usd + instance_cost_usd)

### Scenario 4: Metrics Watcher
1. Scale pod replicas: `kubectl scale deployment test-pod --replicas=3`
2. Dashboard should update within 30 seconds (metrics scrape interval)
3. Verify CPU/Memory gauges reflect all 3 replicas

## Troubleshooting

### Metrics Not Appearing
```bash
# Check if metrics-server is running
kubectl get apiservice v1beta1.metrics.k8s.io

# Check metrics watcher logs
grep "Metrics watcher started" /tmp/aziron-pulse.log

# Verify network collector is scraping cAdvisor
curl http://<node-ip>:4194/metrics
```

### Dashboard Variables Not Populating
```bash
# Ensure metrics have labels
curl http://localhost:8081/metrics | grep "user_email="
```

### Traces Not Showing Span Links
```bash
# Check TTL cleanup span in Tempo
curl http://localhost:3200/api/search?q='{name="ttl.cleanup_pod"}' | jq
```

## Quick Validation Queries

### Prometheus Query Examples
```promql
# Active pods by user
sum(aziron_pulse_pod_active_count) by (user_email)

# Total cost per user (last hour)
sum by (user_email) (increase(aziron_pulse_cost_resource_usd_total[1h]))

# K8s API success rate
sum(rate(aziron_pulse_k8s_api_requests_total{status="success"}[5m])) 
/ sum(rate(aziron_pulse_k8s_api_requests_total[5m])) * 100

# External egress per user
sum by (user_email) (rate(aziron_pulse_network_egress_external_bytes_total[5m]))
```

### TraceQL Query Examples
```traceql
# Find all TTL cleanup operations with span links
{span.kind="server" && name=~"ttl.cleanup_pod" && rootServiceName="aziron-pulse"}

# Find provision operations with cost tracking
{name="provision_pod" && span.cost_resource_usd > 0}
```

## Expected Results

After 5 minutes of operation:
- **Metrics Watcher**: CPU/Memory gauges updating every 30s
- **Network Collector**: External egress metrics from cAdvisor
- **Cost Tracking**: Dual cost counters incrementing
- **Span Links**: TTL cleanup traces linked to provision spans
- **Trace Exemplars**: Click metrics to jump to traces

## Dashboard Refresh

- **Auto-refresh**: 30 seconds (configured in dashboard)
- **Time Range**: Last 6 hours (default)
- **Template Variables**: user_email, tenant_id, namespace (multi-select)
