# ---------- 阶段 1：构建前端 ----------
FROM node:22-alpine AS web-builder

WORKDIR /build/web

# 先只复制依赖清单，让 npm ci 这一层能被 Docker 缓存住
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund

COPY web/ ./
# 构建产物会输出到 ../server/internal/web/dist（由 vite.config.ts 指定），
# 因此需要先把该目录准备好
RUN mkdir -p /build/server/internal/web && npm run build:only


# ---------- 阶段 2：构建后端 ----------
FROM golang:1.24-alpine AS go-builder

WORKDIR /build/server

RUN apk add --no-cache git ca-certificates tzdata

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
# 用前端构建产物覆盖占位页
COPY --from=web-builder /build/server/internal/web/dist ./internal/web/dist

# CGO_ENABLED=0：SQLite 用的是纯 Go 驱动（glebarez/modernc），
# 因此可以静态编译，产物能在 scratch/alpine 里直接跑。
# -trimpath 去掉构建机的绝对路径，-s -w 去掉符号表，减小体积也少泄露信息。
ARG VERSION=1.0.0
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}" \
      -o /build/moecard ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w" \
      -o /build/moecard-migrate ./cmd/migrate


# ---------- 阶段 3：运行时 ----------
FROM alpine:3.20

# tzdata 是必需的：商城时区、支付宝签名的北京时间都依赖它
RUN apk add --no-cache ca-certificates tzdata wget \
 && addgroup -g 1000 moecard \
 && adduser -D -u 1000 -G moecard moecard

WORKDIR /app

COPY --from=go-builder /build/moecard         /app/moecard
COPY --from=go-builder /build/moecard-migrate /app/moecard-migrate

# 数据与上传目录必须挂载出去，否则容器重建会丢数据
RUN mkdir -p /app/data /app/storage/uploads /app/logs \
 && chown -R moecard:moecard /app

USER moecard

ENV APP_ENV=production \
    APP_HOST=0.0.0.0 \
    APP_PORT=8080 \
    DB_DRIVER=sqlite \
    SQLITE_PATH=/app/data/moecard.db \
    STORAGE_LOCAL_PATH=/app/storage/uploads \
    LOG_LEVEL=info \
    LOG_FORMAT=json

EXPOSE 8080

VOLUME ["/app/data", "/app/storage"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/moecard"]
