# Aziron Pulse - Kubernetes Pod Provisioning Service

Aziron Pulse is a microservice that dynamically provisions Kubernetes pods with VS Code (code-server) for development workspaces. It provides secure, isolated development environments with resource quotas, TTL management, and comprehensive observability.

## Features

- **Dynamic Pod Provisioning**: Provision Kubernetes pods with VS Code on-demand
- **Authentication & Authorization**: JWT-based authentication with user/tenant isolation
- **Resource Quotas**: Configurable CPU, memory, storage, and pod count limits per user
- **TTL Management**: Automatic cleanup of inactive pods after 2 hours
- **Persistent Storage**: Workspace data persisted with PersistentVolumes
- **Reverse Proxy**: Transparent proxy to code-server instances with WebSocket support
- **Multi-Tenancy**: Namespace isolation per tenant
- **Observability**: Full OpenTelemetry integration (traces, metrics, logs)
- **Security**: Non-root pods, namespace isolation, resource limits

## Architecture

```
┌─────────────┐       ┌──────────────┐       ┌────────────────┐
│ aziron-ui   │──────▶│ aziron-server│──────▶│ aziron-pulse   │
└─────────────┘       └──────────────┘       └────────────────┘
                                                      │
                                                      ▼
                                              ┌──────────────┐
                                              │  Kubernetes  │
                                              │   Cluster    │
                                              └──────────────┘
                                                      │
                                              ┌───────┴───────┐
                                              │               │
                                          ┌───▼────┐   ┌─────▼────┐
                                          │ Pod 1  │   │  Pod 2   │
                                          │VS Code │   │ VS Code  │
                                          └────────┘   └──────────┘
```

## API Endpoints

### Public Endpoints

- `GET /health` - Health check
- `GET /health/ready` - Readiness probe
- `GET /health/live` - Liveness probe
- `GET /metrics` - Prometheus metrics

### Authenticated Endpoints (require JWT)

#### Provisioning
- `POST /provision` - Provision a new pod
- `GET /provision` - List all user pods
- `GET /provision/{pulse_id}` - Get pod details
- `DELETE /provision/{pulse_id}` - Delete a pod

#### Proxy (to code-server)
- `GET /pulse/{pulse_id}/*` - Proxy to code-server instance

### Legacy Monitoring
- `GET /pulse/status` - Service status
- `POST /pulse/heartbeat` - Send heartbeat
- `GET /pulse/events` - Get events
- `POST /pulse/events` - Create event

## Quick Start

### Prerequisites

- Go 1.24+
- Kubernetes cluster with kubectl configured
- PostgreSQL 15+
- Docker (optional, for containerized deployment)

### Local Development

1. **Clone the repository**
```bash
git clone git@github.com:MsysTechnologiesllc/aziron-pulse.git
cd aziron-pulse
```

2. **Set up environment**
```bash
cp .env.example .env
# Edit .env with your configuration
```

3. **Initialize database**
```bash
psql -U postgres -d aziron -f db/schema/001_pulse_management.sql
```

4. **Install dependencies**
```bash
go mod tidy
```

5. **Run the service**
```bash
go run cmd/main.go
```

### Docker Compose

```bash
docker-compose up -d
```

This starts:
- aziron-pulse service on port 8081
- PostgreSQL database on port 5432

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PULSE_PORT` | HTTP server port | `8081` |
| `DB_HOST` | PostgreSQL host | `localhost` |
| `DB_PORT` | PostgreSQL port | `5432` |
| `DB_USER` | Database user | `postgres` |
| `DB_PASSWORD` | Database password | - |
| `DB_NAME` | Database name | `aziron` |
| `JWT_SECRET` | JWT signing secret (must match aziron-server) | - |
| `AZIRON_WORKSPACE` | Workspace root path | `/var/aziron/workspace` |
| `BASE_IMAGE` | Default code-server image | `codercom/code-server:latest` |
| `TTL_CHECK_INTERVAL` | TTL check frequency (minutes) | `5` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry collector endpoint | `localhost:4318` |

### Resource Quotas

Default quotas per user:
- Max pods: 3
- CPU limit per pod: 2.0 cores
- Memory limit per pod: 4096 MB
- Storage per pod: 10 GB

Quotas are stored in `pulse.quotas` table and can be customized per user/tenant.

## Database Schema

### Tables

- `pulse.quotas` - User resource quotas
- `pulse.pods` - Pod metadata and status
- `pulse.activities` - Pod activity logs

Schema is automatically created from `db/schema/001_pulse_management.sql`.

## API Usage Examples

### Provision a Pod

```bash
curl -X POST http://localhost:8081/provision \
  -H "Authorization: Bearer <JWT_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "base_image": "codercom/code-server:latest",
    "cpu_limit": 2.0,
    "memory_mb": 4096,
    "storage_gb": 10
  }'
```

Response:
```json
{
  "pulse_id": "abc123",
  "status": "pending",
  "namespace": "pulse-tenant-1a2b3c4d",
  "pod_name": "pulse-abc123",
  "node_port": 30080,
  "workspace_path": "/var/aziron/workspace/user-id/pulse/abc123",
  "expires_at": "2024-01-01T14:00:00Z",
  "created_at": "2024-01-01T12:00:00Z"
}
```

### Access VS Code

Once the pod is running, access VS Code via:
```
http://localhost:8081/pulse/{pulse_id}/
```

The proxy automatically forwards requests to the code-server instance.

### List Pods

```bash
curl http://localhost:8081/provision \
  -H "Authorization: Bearer <JWT_TOKEN>"
```

### Delete Pod

```bash
curl -X DELETE http://localhost:8081/provision/{pulse_id} \
  -H "Authorization: Bearer <JWT_TOKEN>"
```

## TTL Management

Pods are automatically deleted after 2 hours of inactivity. Activity is tracked through:
- Proxy requests to `/pulse/{pulse_id}/*`
- Manual activity updates via API

The TTL manager runs every 5 minutes (configurable) and cleans up expired pods.

## Observability

### Prometheus Metrics

- `aziron_pulse_pods_active_total` - Active pod count
- `aziron_pulse_pods_provisioned_total` - Total provisioned pods
- `aziron_pulse_pods_deleted_total` - Total deleted pods
- `aziron_pulse_pods_expired_total` - Total expired pods
- `aziron_pulse_pod_provision_duration_seconds` - Provision duration histogram
- `aziron_pulse_proxy_requests_total` - Proxy request counter
- `aziron_pulse_resource_usage` - Resource usage gauge

### OpenTelemetry Traces

All HTTP requests are traced with OpenTelemetry. Traces include:
- Request method and path
- Response status
- Duration
- User ID and Pulse ID context

### Logs

Structured JSON logs using Uber Zap with fields:
- Timestamp
- Level (info, warn, error)
- Message
- Context (user_id, pulse_id, etc.)

## Integration with Aziron Server

Add to aziron-server's `main_with_db.go`:

```go
// Pulse proxy
pulseServiceURL := os.Getenv("PULSE_SERVICE_URL")
if pulseServiceURL == "" {
    pulseServiceURL = "http://localhost:8081"
}
pulseTarget, _ := url.Parse(pulseServiceURL)
pulseProxy := httputil.NewSingleHostReverseProxy(pulseTarget)

// Route /api/v1/pulse/* to aziron-pulse
apiRouter.PathPrefix("/pulse").Handler(http.StripPrefix("/api/v1", pulseProxy))
```

## Security Considerations

- **JWT Authentication**: All provisioning and proxy endpoints require valid JWT
- **User Isolation**: Users can only access their own pods
- **Non-Root Pods**: All pods run as UID 1000 (non-root)
- **Namespace Isolation**: Each tenant has a dedicated namespace
- **Resource Limits**: CPU and memory limits enforced on all pods
- **TTL Enforcement**: Automatic cleanup prevents resource exhaustion

## Development

### Project Structure

```
aziron-pulse/
├── cmd/
│   └── main.go                    # Application entry point
├── internal/
│   ├── db/                        # Database connection
│   ├── handlers/                  # HTTP handlers
│   │   ├── provision.go          # Provisioning handlers
│   │   ├── proxy.go              # Proxy handlers
│   │   └── pulse.go              # Legacy handlers
│   ├── k8s/                       # Kubernetes managers
│   │   ├── client.go
│   │   ├── namespace_manager.go
│   │   ├── pod_manager.go
│   │   ├── service_manager.go
│   │   └── volume_manager.go
│   ├── middleware/                # HTTP middleware
│   │   └── auth.go               # JWT authentication
│   ├── models/                    # Data models
│   │   └── pulse_pod.go
│   ├── repository/                # Database repositories
│   │   ├── pod_repository.go
│   │   ├── activity_repository.go
│   │   └── quota_repository.go
│   ├── service/                   # Business logic
│   │   ├── provision_service.go
│   │   └── ttl_manager.go
│   └── telemetry/                 # Observability
│       └── telemetry.go
├── db/
│   └── schema/
│       └── 001_pulse_management.sql
├── docker-compose.yml
├── Dockerfile
├── go.mod
└── README.md
```

### Building

```bash
# Build binary
go build -o bin/aziron-pulse cmd/main.go

# Build Docker image
docker build -t aziron-pulse:latest .
```

### Testing

```bash
# Run tests
go test ./...

# Test with coverage
go test -cover ./...
```

## Troubleshooting

### Pod won't start
- Check Kubernetes events: `kubectl get events -n <namespace>`
- Verify PVC is bound: `kubectl get pvc -n <namespace>`
- Check pod logs: `kubectl logs -n <namespace> <pod-name>`

### Database connection issues
- Verify PostgreSQL is running
- Check connection string in environment variables
- Ensure schema is initialized

### Authentication failures
- Verify JWT_SECRET matches aziron-server
- Check token expiration
- Ensure Authorization header format: `Bearer <token>`

## License

Proprietary - Msys Technologies LLC

## Support

For issues and questions, contact the Aziron development team.
