# MoeCard 常用命令
#
# make          查看所有命令
# make build    构建单二进制（含前端）
# make dev      本地开发（前后端分离热更新）
# make check    格式化 + 静态检查 + 测试

.DEFAULT_GOAL := help
.PHONY: help install build build-web build-server dev dev-web dev-server \
        test test-race test-concurrency check fmt vet lint clean docker-build \
        docker-up docker-down docker-logs migrate migrate-status admin

VERSION ?= 1.0.0
BIN     := moecard
ifeq ($(OS),Windows_NT)
	BIN := moecard.exe
endif

BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GO_LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)

help: ## 显示所有命令
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	 | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ---------- 安装 ----------

install: ## 安装前后端依赖
	cd web && npm install
	cd server && go mod download

## ---------- 构建 ----------

build: build-web build-server ## 构建单二进制（含前端）
	@echo "✓ 构建完成: server/$(BIN)"

build-web: ## 只构建前端（产物输出到 server/internal/web/dist）
	cd web && npm run build

build-server: ## 只构建后端
	cd server && CGO_ENABLED=0 go build -trimpath -ldflags="$(GO_LDFLAGS)" -o $(BIN) ./cmd/server
	cd server && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o moecard-migrate ./cmd/migrate

## ---------- 开发 ----------

dev: ## 提示如何启动开发环境
	@echo "请开两个终端分别运行："
	@echo "  make dev-server   # 后端 :8080"
	@echo "  make dev-web      # 前端 :5173（已配好 /api 代理）"

dev-server: ## 启动后端（开发模式）
	cd server && go run ./cmd/server

dev-web: ## 启动前端热更新
	cd web && npm run dev

## ---------- 测试与检查 ----------

check: fmt vet test ## 格式化 + 静态检查 + 测试

fmt: ## 格式化 Go 代码
	cd server && gofmt -w .
	@cd server && test -z "$$(gofmt -l .)" || (echo "仍有未格式化的文件"; exit 1)

vet: ## Go 静态检查
	cd server && go vet ./...

test: ## 运行全部测试
	cd server && go test ./... -count=1

test-race: ## 竞态检测（需要 CGO / gcc）
	cd server && CGO_ENABLED=1 go test ./... -race -count=1

test-concurrency: ## 只跑并发与幂等测试（最关键的部分）
	cd server && go test ./internal/service/ -v -count=1 \
	  -run 'TestConcurrent|TestPaymentAmount|TestLatePayment|TestOrderExpiry'

lint: ## 前端类型检查
	cd web && npm run type-check

## ---------- 数据库 ----------

migrate: ## 执行数据库迁移
	cd server && go run ./cmd/migrate

migrate-status: ## 查看迁移状态
	cd server && go run ./cmd/migrate -status

admin: ## 创建管理员（用法：make admin U=name P='StrongPass!23'）
	@test -n "$(U)" || (echo "用法: make admin U=用户名 P='密码'"; exit 1)
	@test -n "$(P)" || (echo "用法: make admin U=用户名 P='密码'"; exit 1)
	cd server && go run ./cmd/migrate -create-admin -u "$(U)" -p "$(P)"

## ---------- Docker ----------

docker-build: ## 构建 Docker 镜像
	docker compose build

docker-up: ## 启动（后台运行）
	docker compose up -d

docker-down: ## 停止（数据保留在 volume 中）
	docker compose down

docker-logs: ## 查看日志
	docker compose logs -f moecard

## ---------- 清理 ----------

clean: ## 清理构建产物
	rm -f server/$(BIN) server/moecard-migrate server/moecard-migrate.exe
	rm -rf server/internal/web/dist/assets server/internal/web/dist/index.html \
	       server/internal/web/dist/favicon.svg
	rm -rf web/node_modules/.vite
	@echo "✓ 已清理"
