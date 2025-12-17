# syntax=docker/dockerfile:1.5
FROM golang:1.24-alpine AS builder

# 安装构建依赖
# ARG APK_REPO_MAIN=https://dl-cdn.alpinelinux.org/alpine/v3.19/main
# ARG APK_REPO_COMMUNITY=https://dl-cdn.alpinelinux.org/alpine/v3.19/community

# ARG APK_REPO_MAIN=http://mirrors.aliyun.com/alpine/v3.19/main
# ARG APK_REPO_COMMUNITY=http://mirrors.aliyun.com/alpine/v3.19/community
# RUN --mount=type=cache,target=/var/cache/apk apk add --no-cache --repository=${APK_REPO_MAIN} --repository=${APK_REPO_COMMUNITY} git ca-certificates
RUN --mount=type=cache,target=/var/cache/apk apk add --no-cache  git ca-certificates

# 设置工作目录
WORKDIR /app

# 复制go mod文件
COPY go.mod go.sum ./
ENV GOPROXY=https://proxy.golang.org,direct
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# 复制源代码
COPY . .

# 构建应用
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kyc-service ./cmd/server

# 运行时镜像
FROM alpine:3.19

# 安装运行时依赖
# ARG APK_REPO_MAIN=https://dl-cdn.alpinelinux.org/alpine/v3.19/main
# ARG APK_REPO_COMMUNITY=https://dl-cdn.alpinelinux.org/alpine/v3.19/community
# RUN --mount=type=cache,target=/var/cache/apk apk add --no-cache --repository=${APK_REPO_MAIN} --repository=${APK_REPO_COMMUNITY} ca-certificates tzdata
RUN --mount=type=cache,target=/var/cache/apk apk add --no-cache  ca-certificates tzdata

# 创建非root用户
RUN addgroup -g 1000 -S appuser && \
    adduser -u 1000 -S appuser -G appuser

# 设置工作目录
WORKDIR /app

# 复制二进制文件
COPY --from=builder /app/kyc-service .
COPY --from=builder /app/config.yaml .

# 创建日志目录
RUN mkdir -p /app/logs && chown -R appuser:appuser /app

# 切换到非root用户
USER appuser

# 默认端口（可被环境变量覆盖）
ENV KYC_PORT=8082
EXPOSE 8082
EXPOSE 8092

# 健康检查改为指标端点（避免鉴权导致401）
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD sh -c "wget --no-verbose --tries=1 --spider http://localhost:${KYC_PORT}/metrics || exit 1"

# 启动命令
CMD ["./kyc-service"]
