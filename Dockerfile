# Build on the builder's own platform and cross-compile (pure Go, no cgo).
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /idle-kings ./cmd/idle-kings

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source=https://github.com/shantz14/idle-kings \
      org.opencontainers.image.description="Idle Kings: a Discord idle game of court politics"
COPY --from=build /idle-kings /idle-kings
ENV IDLE_KINGS_DB=/data/idle-kings.db \
    IDLE_KINGS_HEALTH_ADDR=:8081
EXPOSE 8081
VOLUME /data
USER nonroot:nonroot
ENTRYPOINT ["/idle-kings"]
