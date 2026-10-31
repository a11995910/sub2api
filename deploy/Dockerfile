# =============================================================================
# Sub2API Multi-Stage Dockerfile
# =============================================================================
# Stage 1: Build frontend
# Stage 2: Build Go backend with embedded frontend
# Stage 3: Final minimal image
# =============================================================================

ARG NODE_IMAGE=node:24-alpine
ARG GOLANG_IMAGE=golang:1.27.3-alpine
ARG ALPINE_IMAGE=alpine:3.20
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn

# Excel 自动授权运行时：固定依赖版本，携带独立动态链接器供 Alpine 使用。
FROM node:22.22.2-bookworm-slim AS excel-node
FROM python:3.12.12-slim-bookworm AS excel-builder
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libstdc++6 curl && rm -rf /var/lib/apt/lists/*
COPY --from=excel-node /usr/local/bin/node /usr/local/bin/node
COPY --from=excel-node /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm
WORKDIR /build/tosub2
RUN curl -fsSL --retry 3 https://codeload.github.com/poxiao33/toSub2/tar.gz/8548397e89bf80e508eda64a87e0d556d43abc84 | tar -xz --strip-components=1
COPY tools/excel-oauth-runtime/package*.json ./
RUN npm ci --omit=dev --ignore-scripts --no-audit --no-fund
COPY tools/excel-oauth-runtime/requirements.txt /build/requirements.txt
RUN python -m pip install --no-cache-dir --only-binary=:all: -r /build/requirements.txt
COPY tools/excel-oauth-runtime/ /build/tools/
RUN cd /build/tools && EXCEL_TEST_TOSUB2_ROOT=/build/tosub2 python -m unittest discover -p 'test_*.py' -q && node test_openai_excel_password_flow.mjs
RUN python /build/tools/package.py /out
FROM ${ALPINE_IMAGE} AS excel-runtime
COPY --from=excel-builder /out/ /opt/excel-oauth-runtime/
RUN /opt/excel-oauth-runtime/python /opt/excel-oauth-runtime/runner.py --check | grep -q '"ready": true' && NODE_EXECUTABLE=/opt/excel-oauth-runtime/node TOSUB2_PYTHON=/opt/excel-oauth-runtime/python /opt/excel-oauth-runtime/node /opt/excel-oauth-runtime/smoke.mjs && touch /opt/excel-oauth-runtime/ready

# -----------------------------------------------------------------------------
# Stage 1: Frontend Builder
# -----------------------------------------------------------------------------
FROM ${NODE_IMAGE} AS frontend-builder

WORKDIR /app/frontend
ARG NODE_MAX_OLD_SPACE_SIZE=2048
ENV NODE_OPTIONS=--max-old-space-size=${NODE_MAX_OLD_SPACE_SIZE}

# Install pnpm. Keep this pinned to the lockfile-compatible major version so
# Docker builds remain reproducible when pnpm changes config validation rules.
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate

# Install dependencies first (better caching)
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# Copy frontend source and build.
# LegalDocumentView.vue (admin-compliance gate) build-time imports
# ../../../../docs/legal/*.md?raw, so docs/legal/ must sit beside frontend/
# in the image (WORKDIR /app/frontend -> resolves to /app/docs/legal/*.md).
# Copy only that subtree to keep the build dependency minimal.
COPY frontend/ ./
COPY docs/legal/ /app/docs/legal/
RUN pnpm run build

# -----------------------------------------------------------------------------
# Stage 2: Backend Builder
# -----------------------------------------------------------------------------
FROM ${GOLANG_IMAGE} AS backend-builder

# Build arguments for version info (set by CI)
ARG VERSION=
ARG COMMIT=docker
ARG DATE
ARG GOPROXY
ARG GOSUMDB
ARG GOMAXPROCS=2

ENV GOPROXY=${GOPROXY}
ENV GOSUMDB=${GOSUMDB}

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app/backend

# Copy go mod files first (better caching)
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy backend source first
COPY backend/ ./

# Copy frontend dist from previous stage (must be after backend copy to avoid being overwritten)
COPY --from=frontend-builder /app/backend/internal/web/dist ./internal/web/dist

# Build the binary (BuildType=docker disables unsafe in-container binary replacement)
# Version precedence: build arg VERSION > exact git tag > cmd/server/VERSION
RUN VERSION_VALUE="${VERSION}" && \
    if [ -z "${VERSION_VALUE}" ]; then VERSION_VALUE="$(./scripts/resolve-version.sh)"; fi && \
    DATE_VALUE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" && \
    GOMAXPROCS="${GOMAXPROCS}" CGO_ENABLED=0 GOOS=linux go build \
    -tags embed \
    -ldflags="-s -w -X main.Version=${VERSION_VALUE} -X main.Commit=${COMMIT} -X main.Date=${DATE_VALUE} -X main.BuildType=docker" \
    -o /app/sub2api \
    ./cmd/server

# -----------------------------------------------------------------------------
# Stage 3: Final Runtime Image
# -----------------------------------------------------------------------------
FROM ${ALPINE_IMAGE}

# Labels
LABEL maintainer="Wei-Shaw <github.com/Wei-Shaw>"
LABEL description="Sub2API - AI API Gateway Platform"
LABEL org.opencontainers.image.source="https://github.com/Wei-Shaw/sub2api"

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    su-exec \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1000 sub2api && \
    adduser -u 1000 -G sub2api -s /bin/sh -D sub2api

# Set working directory
WORKDIR /app

# 自动授权只执行随镜像构建的固定运行时，不在运行时下载代码。
COPY --from=excel-runtime /opt/excel-oauth-runtime/ /opt/excel-oauth-runtime/

# Copy binary and runtime fallback resources from builder
COPY --from=backend-builder /app/sub2api /app/sub2api
COPY --from=backend-builder --chown=sub2api:sub2api /app/backend/resources /app/resources

# Create data directory
RUN mkdir -p /app/data && chown -R sub2api:sub2api /app

# Copy entrypoint script (fixes volume permissions then drops to sub2api)
COPY deploy/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Expose port (can be overridden by SERVER_PORT env var)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget -q -T 5 -O /dev/null http://localhost:${SERVER_PORT:-8080}/health || exit 1

# Run the application (entrypoint fixes /app/data ownership then execs as sub2api)
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/sub2api"]
