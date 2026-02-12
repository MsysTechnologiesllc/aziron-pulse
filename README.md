# Aziron Pulse Service

A Go microservice for monitoring and heartbeat functionality in the Aziron ecosystem.

## Overview

Aziron Pulse is a lightweight microservice that provides:
- Health monitoring endpoints
- Heartbeat collection and tracking
- Event logging and retrieval
- Service status reporting
- Prometheus metrics

## Features

- ✅ Health check endpoints (health, ready, live)
- ✅ Heartbeat tracking from multiple sources
- ✅ Event storage and retrieval (last 1000 events)
- ✅ Service status and uptime monitoring
- ✅ OpenTelemetry integration for distributed tracing
- ✅ Prometheus metrics endpoint
- ✅ CORS enabled for cross-origin requests
- ✅ Graceful shutdown

## Project Structure

```
aziron-pulse/
├── cmd/
│   └── main.go              # Application entry point
├── internal/
│   ├── handlers/
│   │   └── pulse.go         # HTTP handlers
│   └── telemetry/
│       └── telemetry.go     # OpenTelemetry setup
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
└── README.md
```

## API Endpoints

All endpoints are prefixed with `/pulse`:

### Health Endpoints
- `GET /pulse/health` - Basic health check
- `GET /pulse/health/ready` - Readiness probe
- `GET /pulse/health/live` - Liveness probe

### Monitoring Endpoints
- `GET /pulse/status` - Detailed service status including uptime and event count
- `GET /pulse/metrics` - Prometheus metrics

### Business Logic Endpoints
- `POST /pulse/heartbeat` - Submit a heartbeat
  ```json
  {
    "source": "service-name",
    "message": "heartbeat message",
    "data": {}
  }
  ```
- `GET /pulse/events` - Retrieve recent events
- `POST /pulse/events` - Create a new event
  ```json
  {
    "type": "event-type",
    "message": "event message"
  }
  ```

## Environment Variables

- `PULSE_PORT` - HTTP server port (default: 8081)
- `OTEL_EXPORTER_OTLP_ENDPOINT` - OpenTelemetry collector endpoint (default: localhost:4318)

## Running Locally

### Using Go directly:

```bash
cd aziron-pulse
go mod download
go run cmd/main.go
```

### Using Docker Compose:

```bash
docker-compose up -d
```

## Building

```bash
# Build binary
go build -o bin/aziron-pulse cmd/main.go

# Build with version info
go build -ldflags "-X main.Version=v0.1.0 -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.GitCommit=$(git rev-parse --short HEAD)" -o bin/aziron-pulse cmd/main.go
```

## Docker

Build the Docker image:

```bash
docker build -t aziron-pulse:latest .
```

Run the container:

```bash
docker run -p 8081:8081 aziron-pulse:latest
```

## Integration with Aziron Server

The aziron-server will proxy requests from `/pulse/*` to this microservice. The integration is configured in the main aziron-server's routing.

## Development

### Prerequisites
- Go 1.24.4 or higher
- Docker (optional)

### Running Tests
```bash
go test ./...
```

### Code Structure
- `cmd/main.go` - Main application entry point with server setup
- `internal/handlers/pulse.go` - HTTP request handlers and business logic
- `internal/telemetry/telemetry.go` - OpenTelemetry configuration

## Monitoring

- Access metrics at: `http://localhost:8081/pulse/metrics`
- View events at: `http://localhost:8081/pulse/events`
- Check status at: `http://localhost:8081/pulse/status`

## License

Copyright © 2026 Msys Technologies LLC
