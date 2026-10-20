# Прото-контракты платформы ГАР

`proto/suggest/v1/suggest.proto` — gRPC-контракт сервиса подсказок адресов
(`suggest.v1.Suggester`). Сгенерированный код (`*.pb.go`) закоммичен рядом
с `.proto`.

## Регенерация

Из корня модуля:

```
go generate ./proto/...
```

Директива (см. `proto/generate.go`) собирает плагины `protoc-gen-go` и
`protoc-gen-go-grpc` из tool-зависимостей модуля (`go.mod`, блок `tool`) в
`./bin` и вызывает `go tool buf generate` с шаблоном `buf.gen.yaml`
(`paths=source_relative`, вывод в `proto/`). Каталог `bin/` в .gitignore,
плагины пересобираются при каждой регенерации.

Конфигурация buf: `buf.yaml` (v2, модуль `proto/`) и `buf.gen.yaml` — в корне
репозитория. Проверка контракта без генерации: `go tool buf lint`.
