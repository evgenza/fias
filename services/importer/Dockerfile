# Контекст сборки — корень репозитория (исключения — в .dockerignore).

# Сборка статического бинаря gar-importer.
FROM golang:1.27-alpine AS build
ENV CGO_ENABLED=0
WORKDIR /src
# Кэш слоя зависимостей: манифесты скачиваются до копии исходников.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags "-s -w" -o /out/gar-importer ./cmd/gar-importer

# Минимальный рантайм: корневые сертификаты для TLS и wget для HEALTHCHECK.
FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S gar && adduser -S -G gar -u 10001 gar
WORKDIR /app
# Миграции применяются воркером при старте (каталог относительно WORKDIR).
COPY migrations ./migrations
COPY --from=build /out/gar-importer /usr/local/bin/gar-importer
USER gar
EXPOSE 9211
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
    CMD wget -qO- http://127.0.0.1:9211/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/gar-importer"]
CMD ["worker"]
