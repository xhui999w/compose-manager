# syntax=docker/dockerfile:1.7
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
RUN corepack enable
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM golang:1.23-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend /src/frontend/dist/ ./web/dist/
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/compose-manager ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates docker-cli docker-cli-compose tzdata \
    && addgroup -S compose-manager \
    && adduser -S -G compose-manager -h /app compose-manager
WORKDIR /app
COPY --from=backend /out/compose-manager /usr/local/bin/compose-manager
RUN mkdir -p /data /backups /compose \
    && chown -R compose-manager:compose-manager /data /backups /compose /app
EXPOSE 8080
VOLUME ["/data", "/backups", "/compose"]
ENV CM_LISTEN_ADDR=:8080 \
    CM_DATA_DIR=/data \
    CM_BACKUP_DIR=/backups \
    CM_COMPOSE_ROOTS=/compose \
    CM_DOCKER_HOST=unix:///var/run/docker.sock
USER compose-manager
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -q -O - http://127.0.0.1:8080/api/v1/health || exit 1
ENTRYPOINT ["/usr/local/bin/compose-manager"]

