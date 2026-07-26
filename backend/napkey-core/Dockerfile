# Cross-compile from the build machine's native platform, matching kiro-go's Dockerfile.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app
COPY go.mod ./
# No third-party dependencies, so there is nothing to download. Kept as an
# explicit step so adding one later fails loudly here rather than at build time.
RUN go mod download

COPY . .
# CGO off produces a static binary that runs on a bare alpine image.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o napkey-core .

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata

# Run as a non-root user: this process holds the database credentials and the
# session signing key, so a container escape should not start from uid 0.
RUN addgroup -S napkey && adduser -S -G napkey napkey
USER napkey

WORKDIR /app
COPY --from=builder /app/napkey-core .

EXPOSE 8081

# No volume: all state lives in Postgres, which is the point of this service.
CMD ["./napkey-core"]