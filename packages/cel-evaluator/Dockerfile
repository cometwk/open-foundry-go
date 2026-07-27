# Build stage
# Pinned to a patch release so the baked Go stdlib stays ahead of known CVEs
# (>=1.25.11 fixes CVE-2026-42504; >=1.25.12 fixes CVE-2026-39822, the os.Root
# symlink-following flaw); bump deliberately, not via a floating tag.
FROM golang:1.25.12-alpine AS builder

WORKDIR /app

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build the server + the gRPC health probe. Both are compiled
# in the builder (which has the Go toolchain); only the resulting static
# binaries are copied into the runtime image — never the toolchain itself
# (copying /usr/local/go added ~400MB to the final image as a permanent layer).
# `go install pkg@version` resolves that tool's OWN go.mod, so its transitive
# deps (x/net, grpc) are pinned by the tag we pick — bump the tag to pick up
# their security fixes; our go.mod cannot override them.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /cel-evaluator . \
    && GOBIN=/usr/local/bin CGO_ENABLED=0 go install github.com/grpc-ecosystem/grpc-health-probe@v0.4.53

# Runtime stage
FROM alpine:3.20

ARG GIT_REVISION=unknown
LABEL org.opencontainers.image.revision="${GIT_REVISION}"
ENV GIT_REVISION="${GIT_REVISION}"

RUN apk --no-cache add ca-certificates \
    && adduser -D -u 1000 cel

COPY --from=builder /usr/local/bin/grpc-health-probe /usr/local/bin/grpc-health-probe
COPY --from=builder /cel-evaluator /usr/local/bin/cel-evaluator

USER cel

EXPOSE 50051

ENTRYPOINT ["cel-evaluator"]
