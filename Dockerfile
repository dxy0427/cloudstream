# --- Stage 1: 构建阶段 ---
# 使用Go Alpine镜像（体积小且安全），适配1.22版本编译环境
FROM golang:1.22-alpine AS builder

# 安装CGO编译依赖（SQLite需gcc/musl-dev，避免编译失败）
RUN apk add --no-cache gcc musl-dev sqlite-dev

# 设置工作目录
WORKDIR /app

# 复制依赖文件并下载（利用Docker缓存：依赖不变则不重新执行）
COPY go.mod go.sum ./
RUN go mod download

# 复制全部项目文件
COPY . .

# 编译Go应用：
# - CGO_ENABLED=1：启用CGO（SQLite依赖）
# - -ldflags="-s -w"：去除调试信息，减小可执行文件体积
# - -o /cloudstream：输出到根目录，后续复制更便捷
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /cloudstream ./cmd/cloudstream


# --- Stage 2: 最终运行阶段 ---
# 使用最小Alpine镜像，减少镜像体积和攻击面
FROM alpine:latest

# 安装运行依赖（仅需SQLite库，无需编译工具）
RUN apk add --no-cache sqlite-libs

# 设置工作目录
WORKDIR /app

# 从构建阶段复制编译产物：
# 1. 可执行文件
# 2. 前端静态文件（public目录，UI页面依赖）
COPY --from=builder /cloudstream .
COPY --from=builder /app/public ./public

# 暴露服务端口（与应用内监听端口一致：12398）
EXPOSE 12398

# 容器启动命令（执行应用）
CMD ["./cloudstream"]
