# --- Stage 1: Build Frontend ---
FROM node:20-alpine AS frontend-builder
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

# --- Stage 2: Build Backend ---
FROM golang:1.26-alpine AS backend-builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /cloudstream ./cmd/cloudstream

# --- Stage 3: Final Image ---
FROM alpine:latest
RUN apk add --no-cache sqlite-libs ca-certificates tzdata mailcap && update-ca-certificates
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=backend-builder /cloudstream .
COPY --from=frontend-builder /web/dist ./public
RUN mkdir -p /app/data /app/strm
EXPOSE 12398 8091
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:12398/healthz || exit 1
CMD ["./cloudstream"]
