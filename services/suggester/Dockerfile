# suggester — микросервис подсказок адресов ГАР (gRPC + HTTP health/metrics).
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/suggester ./services/suggester

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
    && adduser -D -u 10001 suggester
COPY --from=build /out/suggester /usr/local/bin/suggester
USER suggester
EXPOSE 50051 9213
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=5 \
    CMD wget -qO- http://127.0.0.1:9213/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/suggester"]
