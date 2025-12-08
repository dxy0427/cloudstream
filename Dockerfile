# --- Stage 1: Build Frontend ---
FROM node:20-alpine AS frontend-builder
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
COPY frontend/ .
RUN npm run build

# --- Stage 2: Build Backend ---
FROM golang:1.22-alpine AS backend-builder
# 安装 SQLite 编译依赖
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# 编译 (开启 CGO 以支持 SQLite)
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /cloudstream ./cmd/cloudstream

# --- Stage 3: Final Image ---
FROM alpine:latest

# 安装 SQLite 运行库和 CA 证书
RUN apk add --no-cache sqlite-libs ca-certificates tzdata && update-ca-certificates

WORKDIR /app

# 复制后端二进制文件
COPY --from=backend-builder /cloudstream .

# 复制前端构建产物到 public 目录 (Gin 将 serve 这个目录)
COPY --from=frontend-builder /web/dist ./public

# 暴露端口
EXPOSE 12398

# 启动
CMD ["./cloudstream"]