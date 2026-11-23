# syntax=docker/dockerfile:1

##############################################
# Builder Stage
##############################################
FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.25.4-alpine AS builder

ARG TARGETPLATFORM BUILDPLATFORM TARGETOS TARGETARCH

RUN apk update && apk add --no-cache make git

WORKDIR /code
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -buildvcs=false \
    -trimpath \
    -ldflags "-s -w" \
    -o prometheus-virtfusion-exporter \
    ./cmd/exporter

##############################################
# Runtime Stage
##############################################
FROM alpine:3.23 AS runtime

WORKDIR /app

COPY --from=builder /code/prometheus-virtfusion-exporter /app/prometheus-virtfusion-exporter

RUN apk update && apk add --no-cache ca-certificates

RUN addgroup -g 1000 exporter && \
    adduser -D -u 1000 -G exporter exporter && \
    chown -R exporter:exporter /app

USER exporter

EXPOSE 9178

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9178/healthz || exit 1

# ENTRYPOINT (not CMD): Kubernetes container.args replace Docker CMD. The Helm
# chart passes --listen/--only-name-prefix/--insecure-skip-tls as args; with
# only CMD those flags become the executable and the pod CrashLoopBackOffs.
ENTRYPOINT ["/app/prometheus-virtfusion-exporter"]
