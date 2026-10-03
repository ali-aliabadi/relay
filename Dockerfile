# syntax=docker/dockerfile:1

# Build a static binary. Digests are pinned and bumped by Dependabot.
FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-s -w -X main.version=${VERSION}" -o /out/relay ./cmd/relay \
    && mkdir -p /out/data

# Distroless static, non-root. Only /data is meant to be writable, so the
# container runs fine with a read-only root filesystem.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/relay /usr/local/bin/relay
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV RELAY_ADDR=:8080 RELAY_DB_PATH=/data/relay.db
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/relay"]
CMD ["serve"]
