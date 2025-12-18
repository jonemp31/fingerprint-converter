# Fingerprint Converter - Makefile

.PHONY: help build run dev docker-build docker-run docker-stop clean test

# Variables
APP_NAME=fingerprint-converter
DOCKER_IMAGE=fingerprint-converter:latest
PORT=5001

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

build: ## Build Go binary
	@echo "🔨 Building $(APP_NAME)..."
	@go build -ldflags="-w -s" -o $(APP_NAME) cmd/api/main.go
	@echo "✅ Build complete: ./$(APP_NAME)"

run: ## Run locally (requires FFmpeg)
	@echo "🚀 Starting $(APP_NAME) on port $(PORT)..."
	@go run cmd/api/main.go

dev: ## Run with auto-reload (requires 'air')
	@echo "🔄 Starting development server with hot reload..."
	@air

docker-build: ## Build Docker image
	@echo "🐳 Building Docker image..."
	@docker build -t $(DOCKER_IMAGE) .
	@echo "✅ Docker image built: $(DOCKER_IMAGE)"

docker-run: ## Run with docker-compose
	@echo "🐳 Starting services with docker-compose..."
	@docker-compose up -d
	@echo "✅ Services started! Logs: docker-compose logs -f"

docker-stop: ## Stop docker-compose services
	@echo "🛑 Stopping services..."
	@docker-compose down
	@echo "✅ Services stopped"

docker-logs: ## View docker logs
	@docker-compose logs -f

clean: ## Clean build artifacts
	@echo "🧹 Cleaning..."
	@rm -f $(APP_NAME)
	@rm -rf /tmp/media-cache/*
	@echo "✅ Cleaned"

test: ## Run tests
	@echo "🧪 Running tests..."
	@go test -v ./...

deps: ## Download dependencies
	@echo "📦 Downloading dependencies..."
	@go mod download
	@go mod tidy
	@echo "✅ Dependencies ready"

fmt: ## Format code
	@echo "✨ Formatting code..."
	@go fmt ./...
	@echo "✅ Code formatted"

lint: ## Run linter (requires golangci-lint)
	@echo "🔍 Running linter..."
	@golangci-lint run ./...

health: ## Check API health
	@echo "❤️  Checking health..."
	@curl -s http://localhost:$(PORT)/api/health | jq

stats: ## Get cache stats
	@echo "📊 Getting cache stats..."
	@curl -s http://localhost:$(PORT)/api/cache/stats | jq

example-audio: ## Test audio conversion
	@echo "🎵 Testing audio conversion..."
	@curl -X POST http://localhost:$(PORT)/api/convert \
		-H "Content-Type: application/json" \
		-d '{"device_id":"test","url":"https://example.com/audio.mp3","media_type":"audio","anti_fingerprint_level":"moderate"}' | jq

example-image: ## Test image conversion
	@echo "🖼️  Testing image conversion..."
	@curl -X POST http://localhost:$(PORT)/api/convert \
		-H "Content-Type: application/json" \
		-d '{"device_id":"test","url":"https://example.com/image.jpg","media_type":"image","anti_fingerprint_level":"moderate"}' | jq

install-tools: ## Install development tools
	@echo "🔧 Installing development tools..."
	@go install github.com/cosmtrek/air@latest
	@echo "✅ Tools installed"
