# syntax=docker/dockerfile:1

# Build a static binary. Digests are pinned and bumped by Dependabot.
FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-s -w -X main.version=${VERSION}" -o /out/relay ./cmd/relay \
    && mkdir -p /out/data

# Distroless static, non-root. Only /data is meant to be writable, so the
# container runs fine with a read-only root filesystem.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/relay /usr/local/bin/relay
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV RELAY_ADDR=:8080 RELAY_DB_PATH=/data/relay.db
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/relay"]
CMD ["serve"]
