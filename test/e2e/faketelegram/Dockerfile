# Fake Telegram Bot API for the e2e suite. Build from the repo root:
#   docker build -f test/e2e/faketelegram/Dockerfile -t relay-faketelegram:e2e .
FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
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
