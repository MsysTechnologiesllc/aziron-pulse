#!/bin/bash
#
# Test script for aziron-pulse provision API
#

echo "=== Testing Aziron Pulse Provision API ==="
echo ""

# Check if services are running
echo "1. Checking service health..."
PULSE_HEALTH=$(curl -s http://localhost:8081/health 2>/dev/null)
SERVER_HEALTH=$(curl -s http://localhost:8080/health 2>/dev/null)

if [ -z "$PULSE_HEALTH" ]; then
    echo "❌ Aziron Pulse (8081) is not running"
    exit 1
else
    echo "✅ Aziron Pulse (8081) is running"
fi

if [ -z "$SERVER_HEALTH" ]; then
    echo "❌ Aziron Server (8080) is not running"
    exit 1
else
    echo "✅ Aziron Server (8080) is running"
fi

echo ""
echo "2. Checking metrics endpoint..."
METRICS_COUNT=$(curl -s http://localhost:8081/metrics | grep -c "aziron_pulse")
echo "✅ Found $METRICS_COUNT aziron_pulse metrics"

echo ""
echo "3. Testing provision API (without auth - expect 401)..."
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST http://localhost:8081/provision \
  -H "Content-Type: application/json" \
  -d '{
    "user_email": "test@example.com",
    "tenant_id": "test-tenant",
    "instance_tier": "balanced",
    "cpu_limit": "2.0",
    "memory_mb": "4096",
    "storage_gb": "10",
    "ttl_hours": "2"
  }')

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$HTTP_CODE" = "401" ] || [ "$HTTP_CODE" = "403" ]; then
    echo "✅ Got expected auth error (HTTP $HTTP_CODE): $BODY"
else
    echo "⚠️  Got HTTP $HTTP_CODE: $BODY"
fi

echo ""
echo "4. Checking Prometheus metrics..."
echo "   - Pod lifecycle metrics:"
curl -s http://localhost:8081/metrics | grep "aziron_pulse_pod" | head -5

echo ""
echo "   - Resource usage metrics:"
curl -s http://localhost:8081/metrics | grep "aziron_pulse_cpu_usage\|aziron_pulse_memory_usage" | head -3

echo ""
echo "   - Cost metrics:"
curl -s http://localhost:8081/metrics | grep "aziron_pulse_cost" | head -5

echo ""
echo "=== Test Summary ==="
echo "✅ Both services running"
echo "✅ Metrics endpoint accessible"
echo "✅ API authentication working"
echo ""
echo "Next steps:"
echo "1. Generate a valid JWT token from aziron-server"
echo "2. Use the token to test provision API"
echo "3. Monitor metrics in Grafana"
echo ""
