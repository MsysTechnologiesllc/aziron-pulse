BINARY_NAME=aziron-pulse
VERSION?=v0.1.0
BUILD_TIME=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS=-ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}"

.PHONY: all build run clean test docker-build docker-run help

all: build

## build: Build the application
build:
	@echo "Building ${BINARY_NAME}..."
	@go build ${LDFLAGS} -o bin/${BINARY_NAME} cmd/main.go
	@echo "Build complete: bin/${BINARY_NAME}"

## run: Run the application
run:
	@echo "Running ${BINARY_NAME}..."
	@go run ${LDFLAGS} cmd/main.go

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@go clean
	@echo "Clean complete"

## test: Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	@docker build -t ${BINARY_NAME}:${VERSION} .
	@docker tag ${BINARY_NAME}:${VERSION} ${BINARY_NAME}:latest
	@echo "Docker image built: ${BINARY_NAME}:${VERSION}"

## docker-run: Run Docker container
docker-run:
	@echo "Running Docker container..."
	@docker run -p 8081:8081 --name ${BINARY_NAME} ${BINARY_NAME}:latest

## docker-compose-up: Start services with docker-compose
docker-compose-up:
	@echo "Starting services..."
	@docker-compose up -d

## docker-compose-down: Stop services with docker-compose
docker-compose-down:
	@echo "Stopping services..."
	@docker-compose down

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'
