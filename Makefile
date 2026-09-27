.PHONY: build test run lint clean dev fmt vet tidy help

# Binary name
BINARY_NAME=router
BUILD_DIR=bin

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet

# Build flags
CGO_ENABLED=1
LDFLAGS=-ldflags "-s -w"

# Default target
all: build

## build: Build the application
build:
	@echo "Building..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/router

## test: Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -cover ./...

## test-coverage: Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## run: Run the application
run:
	@echo "Running..."
	$(GOCMD) run ./cmd/router

## dev: Run with development config
dev:
	@echo "Running in development mode..."
	$(GOCMD) run ./cmd/router -config config.example.yaml

## lint: Run linter
lint:
	@echo "Linting..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

## fmt: Format code
fmt:
	@echo "Formatting..."
	$(GOFMT) ./...

## vet: Run go vet
vet:
	@echo "Vetting..."
	$(GOVET) ./...

## tidy: Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t local-model-router:latest .

## docker-run: Run with Docker Compose
docker-run:
	@echo "Starting with Docker Compose..."
	docker-compose up -d

## docker-stop: Stop Docker Compose
docker-stop:
	@echo "Stopping Docker Compose..."
	docker-compose down

## help: Show this help
help:
	@echo "Local Model Router - Available targets:"
	@echo ""
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
