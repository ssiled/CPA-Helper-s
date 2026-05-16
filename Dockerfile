# syntax=docker/dockerfile:1.7

FROM node:20-bookworm-slim AS frontend-build

WORKDIR /app/frontend

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY VERSION ../VERSION
COPY frontend/ ./
RUN npm run build


FROM golang:1.26-bookworm AS backend-build

WORKDIR /app/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cpa-helper ./cmd/cpa-helper


FROM debian:bookworm-slim AS runtime

ENV CPA_HELPER_DATA_DIR=/app/data \
    CPA_HELPER_FRONTEND_DIST=/app/frontend/dist

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

COPY --from=backend-build /out/cpa-helper /app/cpa-helper
COPY --from=frontend-build /app/frontend/dist /app/frontend/dist

EXPOSE 18317

CMD ["/app/cpa-helper"]
