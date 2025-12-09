# --- Stage 1: Build Frontend ---
FROM node:20-alpine AS frontend-builder
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
COPY frontend/ .
RUN npm run build

# --- Stage 2: Build Backend ---
FROM golang:1.22-alpine AS backend-builder
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /cloudstream ./cmd/cloudstream

# --- Stage 3: Final Image ---
FROM alpine:latest

# 安装必要依赖
# tzdata: 时区数据
# sqlite-libs: 数据库依赖
# ca-certificates: HTTPS 证书
RUN apk add --no-cache sqlite-libs ca-certificates tzdata mailcap && update-ca-certificates

# 核心修复：设置时区为上海时间
ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=backend-builder /cloudstream .
COPY --from=frontend-builder /web/dist ./public

EXPOSE 12398

CMD ["./cloudstream"]