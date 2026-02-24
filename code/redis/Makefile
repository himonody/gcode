.PHONY: help build run test clean docker-up docker-down lint fmt

help: ## 显示帮助信息
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## 编译应用
	@echo "Building application..."
	@go build -o bin/redis-app main.go
	@echo "Build complete: bin/redis-app"

run: ## 运行应用
	@echo "Running application..."
	@go run main.go

test: ## 运行测试
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-unit: ## 运行单元测试
	@echo "Running unit tests..."
	@go test -v -short ./...

bench: ## 运行基准测试
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

clean: ## 清理构建文件
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

lint: ## 运行代码检查
	@echo "Running linter..."
	@golangci-lint run ./...

fmt: ## 格式化代码
	@echo "Formatting code..."
	@go fmt ./...
	@goimports -w .

vet: ## 运行 go vet
	@echo "Running go vet..."
	@go vet ./...

tidy: ## 整理依赖
	@echo "Tidying dependencies..."
	@go mod tidy
	@go mod verify

docker-up: ## 启动 Docker Redis
	@echo "Starting Redis with Docker..."
	@docker-compose up -d
	@echo "Redis started on localhost:6379"

docker-down: ## 停止 Docker Redis
	@echo "Stopping Redis..."
	@docker-compose down
	@echo "Redis stopped"

docker-logs: ## 查看 Redis 日志
	@docker-compose logs -f redis

install-tools: ## 安装开发工具
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@echo "Tools installed"

dev: docker-up run ## 开发模式（启动 Redis 并运行应用）

prod-build: ## 生产环境构建
	@echo "Building for production..."
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/redis-app-linux main.go
	@echo "Production build complete: bin/redis-app-linux"

.DEFAULT_GOAL := help
