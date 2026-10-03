# Fake Telegram Bot API for the e2e suite. Build from the repo root:
#   docker build -f test/e2e/faketelegram/Dockerfile -t relay-faketelegram:e2e .
FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -o /out/faketelegram ./test/e2e/faketelegram

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/faketelegram /faketelegram
USER 65532:65532
EXPOSE 8081
ENTRYPOINT ["/faketelegram"]
