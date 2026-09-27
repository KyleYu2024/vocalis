# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

# Injected by BuildKit for the target platform (defaults to the build host).
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
RUN apk add --no-cache git ca-certificates

# Cache the module download separately from the source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/vocalis ./cmd/vocalis

# ---- runtime stage ---------------------------------------------------------
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /out/vocalis /usr/local/bin/vocalis

ENV VOCALIS_LIBRARY_DIR=/audiobooks \
    VOCALIS_DATA_DIR=/data \
    VOCALIS_ADDR=:8080 \
    TZ=Asia/Shanghai

VOLUME ["/audiobooks", "/data"]
EXPOSE 8080

HEALTHCHECK --interval=60s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/vocalis"]
