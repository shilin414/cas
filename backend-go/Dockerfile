# xiaoan-backend — one image, five entrypoints (api | stream | worker | scheduler | migrate).
# Kubernetes selects the process via `command:`; build once per release tag.
# Build context must be backend-go/ (docker build -f backend-go/Dockerfile backend-go).

FROM golang:1.27.1-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git ca-certificates

# Dependency layer first so source edits do not invalidate `go mod download`.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/stream ./cmd/stream \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/scheduler ./cmd/scheduler \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 xiaoan \
    && adduser -D -u 10001 -G xiaoan xiaoan

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=builder /out/api /usr/local/bin/api
COPY --from=builder /out/stream /usr/local/bin/stream
COPY --from=builder /out/worker /usr/local/bin/worker
COPY --from=builder /out/scheduler /usr/local/bin/scheduler
COPY --from=builder /out/migrate /usr/local/bin/migrate

# Migrations are applied manually by ops (never by Jenkins or at pod startup);
# shipping them in the image makes `kubectl run studio-migrate -- -dir /app/db/migrations`
# runnable without a source checkout.
COPY --from=builder /build/db/migrations /app/db/migrations

RUN mkdir -p /app/data/storage \
    && chown -R 10001:10001 /app

USER 10001

EXPOSE 8080 8081

# Plain `docker run` starts the API; K8s Deployments override `command:`.
CMD ["api"]
