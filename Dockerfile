FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /idle-kings ./cmd/idle-kings

FROM gcr.io/distroless/static-debian12
COPY --from=build /idle-kings /idle-kings
ENV IDLE_KINGS_DB=/data/idle-kings.db
VOLUME /data
ENTRYPOINT ["/idle-kings"]
