# indexer — микросервис поискового индекса адресов ГАР.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/indexer ./services/indexer

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
    && adduser -D -u 10001 indexer
COPY --from=build /out/indexer /usr/local/bin/indexer
USER indexer
EXPOSE 9212
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=5 \
    CMD wget -qO- http://127.0.0.1:9212/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/indexer"]
