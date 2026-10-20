# address-api — публичный REST API адресного поиска ГАР: h2c-порт (HTTP/1.1 +
# HTTP/2 prior-knowledge), статика (Swagger UI, веб-клиент) встроена в бинарь.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/address-api ./services/address-api

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
    && adduser -D -u 10001 address-api
COPY --from=build /out/address-api /usr/local/bin/address-api
USER address-api
EXPOSE 8080 9214
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=5 \
    CMD wget -qO- http://127.0.0.1:9214/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/address-api"]
