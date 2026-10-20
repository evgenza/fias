# GAR Address Platform

Микросервисная платформа адресного поиска по данным [ГАР (ФИАС)](https://fias.nalog.ru/Frontend):
импорт XML-выгрузки из распределённого хранилища в PostgreSQL через брокер сообщений,
построение поискового индекса в Elasticsearch, публичный REST API (HTTP/2, Swagger)
и gRPC API подсказок (unary + стриминг), веб-клиент, логи в Elastic, бизнес-метрики
в Prometheus, трейсы в Jaeger.

Репозиторий вырос из `gar-importer` — потокового импортёра выгрузки ГАР в PostgreSQL
(см. раздел «Импортёр ГАР» ниже; подробности форматов и модели данных — в `docs/`).

## Архитектура

```
                        ┌────────────────────────────────────────────────────┐
                        │                     MinIO (S3)                     │
                        │            ZIP-архивы выгрузки ГАР                 │
                        └───────────────────────┬────────────────────────────┘
  make seed-fixture /                         │ скачивание архива
  seed-demo.sh публикует                      ▼
  gar.import.requested ──▶ NATS ──▶ ┌──────────────────┐    upsert пакетами
                      JetStream     │     importer     │ ──▶ PostgreSQL (Ent,
                      (durable)     │  (gar-importer   │     схема gar,
                                    │   worker, Go)    │     журнал gar_control)
                                    └────────┬─────────┘
                                             │ события gar.objects.changed
                                             ▼
                                    ┌──────────────────┐    bulk-индексация
                                    │     indexer      │ ──▶ Elasticsearch
                                    │       (Go)       │     (gar-addresses)
                                    └──────────────────┘            ▲
                                          читает gar.*              │
┌───────────┐    REST (HTTP/1.1 + HTTP/2)   ┌──────────────┐        │
│  Браузер  │ ────────────────────────────▶ │ address-api  │        │
│ веб-клиент│                               │    (Go)      │        │
└───────────┘                               └──┬────────┬──┘        │
                                               │        │ gRPC      │
                                               │        ▼           │
                                               │   ┌──────────────┐ │
                                               │   │  suggester   │─┘
                                               │   │  (Go, unary  │  запросы
                                               │   │ + streaming) │
                                               │   └──────────────┘
                                               ▼
                                          Redis (кэш карточек)

Наблюдаемость: все сервисы пишут JSON-логи (slog) → Filebeat → Elasticsearch → Kibana;
метрики Prometheus (/metrics) → Grafana (дашборды как код); трейсы OTLP → Jaeger.
```

## Требования

- Docker с Docker Compose (стек занимает ~5–6 ГБ RAM; рекомендуется ≥8 ГБ,
  выделенных Docker).
- Для разработки и демо-фикстуры: Go 1.27.1+.
- Для полного демо с реальными данными: файл полной выгрузки `gar_xml.zip`
  (скачивается с сайта ФИАС, ~57 ГБ).

## Быстрый старт

```sh
cp .env.example .env        # дефолты годятся для локального запуска
make up                     # сборка образов и подъём всего стека
make seed-fixture           # демо-данные: синтетическая фикстура (сотни записей, ~1 мин)
```

Открыть:

- **Веб-клиент**: http://localhost:8080 — поиск с автодополнением, карточка адреса.
- **Swagger UI**: http://localhost:8080/swagger/ — спецификация `api/openapi/address-api.yaml`.
- **Grafana**: http://localhost:3000 (admin / см. `.env`) — дашборды «GAR Platform — …».
- **Kibana**: http://localhost:5601 — data view `gar-logs-*` создаётся автоматически.
- **Jaeger**: http://localhost:16686 — сквозные трейсы REST → gRPC → ES.
- **Prometheus**: http://localhost:9090 — targets всех сервисов и экспортёров.

Проверка без ручных шагов: `make smoke-seed` — генерирует фикстуру, прогоняет
конвейер MinIO → NATS → PostgreSQL → Elasticsearch и проверяет все точки входа.

### Демо на реальных данных (регион 77 — Москва)

```sh
make seed-demo   # соберёт build/gar-demo.zip из ~/Downloads/gar_xml.zip и поставит задачу импорта
```

`scripts/make-demo-zip.sh` извлекает из полного архива только корневые справочники
и каталог региона (по central directory, без распаковки 57 ГБ целиком).
Импорт региона занимает десятки минут; прогресс: `docker compose logs -f importer indexer`,
наполнение индекса: `curl -s localhost:9200/gar-addresses/_count | jq .count`.

После обновления с физического индекса `gar-addresses` выполните
[миграцию поискового индекса v2](docs/search-index-migration.md) при остановленном
indexer. Новая установка создаёт write alias автоматически.

## Сервисы

| Сервис | Исходники | Назначение | Порты (localhost) |
|---|---|---|---|
| `importer` | `cmd/gar-importer` + `internal/worker` | импорт ZIP из MinIO → PostgreSQL, события в NATS | 9211 (metrics/health) |
| `indexer` | `services/indexer` | события NATS → сборка адресов из gar.* → bulk в Elasticsearch | 9212 |
| `suggester` | `services/suggester` + `proto/suggest/v1` | gRPC: `GetAddress`, `Suggest`, `StreamSuggestions` (стриминг) | 50051, 9213 |
| `address-api` | `services/address-api` | REST API + HTTP/2 + Swagger UI + веб-клиент, кэш Redis | 8080 (public), 9214 (internal metrics/health) |

Инфраструктура: PostgreSQL 18, Elasticsearch 8.15, Kibana, Filebeat,
NATS JetStream, MinIO, Redis 7, Prometheus, Grafana, Jaeger + экспортёры
метрик (postgres, redis, elasticsearch, nats, minio).

## API

### REST (HTTP/1.1 и HTTP/2 на одном порту 8080)

```sh
curl --http2-prior-knowledge 'http://localhost:8080/api/v1/suggest?q=тверская&limit=5' | jq
curl -s http://localhost:8080/api/v1/addresses/201 | jq          # карточка (X-Cache: miss→hit)
curl -s http://localhost:8080/api/v1/addresses/103/children | jq # дети по иерархии
```

Спецификация — `api/openapi/address-api.yaml`, UI — `/swagger/`.

### gRPC (порт 50051, reflection включён)

```sh
grpcurl -plaintext localhost:50051 list                                  # список сервисов
grpcurl -plaintext -d '{"query":"твер","limit":3}' \
  localhost:50051 suggest.v1.Suggester/StreamSuggestions                 # server-streaming
grpcurl -plaintext -d '{"object_id":"201"}' \
  localhost:50051 suggest.v1.Suggester/GetAddress                        # unary
```

## Соответствие требованиям

| Требование | Где реализовано |
|---|---|
| ≥3 микросервиса в исходниках на Go | `importer`, `indexer`, `suggester`, `address-api` (4) |
| Запуск в Docker | `compose.yaml`, `make up`; сборка из исходников (`build:` секции) |
| Самоинициализация БД | миграции при старте `importer` и one-shot `migrate`; индекс ES создаёт `indexer`; бакет — `minio-init`; data view — `kibana-init` |
| Интеграционные тесты | testcontainers (NATS/MinIO/PostgreSQL/Elasticsearch/Redis): `make test-integration`; e2e `scripts/smoke.sh --with-seed` |
| HTTP/2, REST API | `address-api` (unencrypted HTTP/2 + HTTP/1.1 на :8080) |
| Swagger API | `api/openapi/address-api.yaml` + завендоренный Swagger UI на `/swagger/` |
| gRPC: запросы и стриминг | `proto/suggest/v1/suggest.proto`: unary `GetAddress`/`Suggest`, server-streaming `StreamSuggestions` |
| Реляционная БД и NoSQL | PostgreSQL (Ent/Atlas) + Elasticsearch (поиск) + Redis (кэш) |
| Брокер сообщений | NATS JetStream (stream GAR, durable consumers, ack/nak/term, redelivery) |
| Логи в Elastic | slog JSON → Filebeat (autodiscover) → `gar-logs-*` → Kibana |
| Бизнес-метрики в Prometheus | `gar_import_*`, `indexer_*`, `address_api_*`, `grpc_*` на `/metrics` всех сервисов |
| Распределённое хранилище | MinIO (S3): архивы ГАР, бакет `gar-archives` |
| ★ Дашборды | Grafana provisioning: `deploy/grafana/dashboards/json/` (Основной, Сервисы, Инфраструктура); Kibana data view |
| ★ Телеметрия | OpenTelemetry → Jaeger, сквозные трейсы REST→gRPC→ES |
| ★ Веб-клиент | `services/address-api/static/web/` (vanilla JS, embed) |
| ★ Метрики БД/брокера/кэша | экспортёры в compose + часть на главном дашборде «GAR Platform — Основной» |
| ★★ Нагрузочное тестирование | `tests/load/` (k6), отчёт `docs/report/load-testing.md` |
| ★★ Отказоустойчивость | `scripts/chaos/`, отчёт `docs/report/fault-tolerance.md` |

Отчёты: `docs/report/` — `architecture.md`, `demo.md`, `load-testing.md`,
`fault-tolerance.md` (со скриншотами в `docs/report/img/`).

## Разработка

```sh
make build            # go build ./...
make test             # юнит-тесты
make test-integration # интеграционные (нужен Docker; testcontainers)
make vet              # go vet (оба набора тегов)
make proto            # регенерация gRPC-кода из proto/suggest/v1 (buf как go tool)
```

Линтер: `golangci-lint run` (конфиг `.golangci.yml`, v2; 0 issues).

CI: `.github/workflows/ci.yml` — три job'а: lint+unit+build, интеграционные
тесты (testcontainers), compose-smoke (подъём стека, засев фикстурой, smoke).
Все этапы должны быть зелёными без предупреждений.

### Структура репозитория

```
cmd/gar-importer     — CLI: migrate, import (локальный ZIP), worker, request-import
cmd/gar-gen-spec     — генерация спецификаций атрибутов из закреплённых XSD
cmd/gar-gen-records  — генерация независимых записей из моделей Ent
cmd/gar-mkfixture    — генерация маленькой валидной фикстуры ZIP (CI, демо)
cmd/gar-migrate-diff — генерация следующей миграции сравнением с dev-базой
ent/schema           — схемы 23 типов файлов ГАР; ent/ — сгенерированный код
internal/cli         — разбор флагов команд, коды завершения
internal/config      — env/.env, пул соединений, маскирование DSN
internal/control     — журнал импорта gar_control: запуски, файлы, checkpoint
internal/gar         — каталог форматов, план архива, валидация по XSD-спекам
internal/gar/record  — независимые типизированные записи и потоковый декодер
internal/garxml      — потоковое декодирование XML по записям
internal/gartype     — Date, Flag, Integer: XML/JSON и SQL-представление
internal/importjob   — сценарий импорта (порты Archive/Writer/Journal, события)
internal/input/zipgar — источник: инвентаризация ZIP, открытие XML
internal/storage     — Ent-клиент, пул pgx, пакетный writer
internal/migrations  — версионированные Atlas-миграции (up/down, advisory lock)
internal/platform    — slog JSON, Prometheus-реестр, OTel, HTTP health, graceful
internal/events      — NATS JetStream: ImportRequested/ObjectsChanged, pub/sub
internal/objectstorage — MinIO/S3: скачивание архива, OpenArchive
internal/search      — Elasticsearch: документ, индекс gar-addresses, bulk
internal/address     — сборка полного адреса из gar.* (PATH, типы, справочники)
internal/worker      — фоновый исполнитель задач импорта (NATS → MinIO → PG)
services/indexer     — потребитель gar.objects.changed → Elasticsearch
services/suggester   — gRPC подсказки (unary + server-streaming)
services/address-api — REST/HTTP2/Swagger/веб-клиент, кэш Redis
proto/suggest/v1     — контракт gRPC + сгенерированный код (go generate ./proto/...)
api/openapi          — спецификация REST API
deploy/              — prometheus.yml, filebeat.yml, grafana, kibana-init
scripts/             — seed-demo.sh, make-demo-zip.sh, smoke.sh, chaos/
tests/load           — k6-сценарии и результаты прогонов
docs/                — форматы, решения, отчёты (docs/report)
migrations/          — Atlas-история схемы gar, legacy-baseline
xsd/                 — закреплённые XSD ФИАС
```

## Импортёр ГАР (CLI)

CLI остаётся самостоятельным инструментом: `migrate`, `import` (локальный ZIP),
`worker` (задачи из NATS), `request-import` (постановка задачи).

```sh
docker compose up -d --wait postgres
go run ./cmd/gar-importer migrate
go run ./cmd/gar-importer import --zip "$HOME/Downloads/gar_xml.zip" --regions 77
```

Ключевые свойства: потоковое чтение ZIP без распаковки на диск; пакетная запись
через `CreateBulk`+upsert (повторный импорт обновляет записи); журнал `gar_control`
с checkpoint на файл и возобновлением `--resume`; валидация записей по XSD-спекам
до записи; UTF-8 и Windows-1251. Модели и особенности типов — в таблице
«Модели ↔ форматы» ниже и в `docs/baseline.md`.

<details>
<summary>Модели ↔ форматы (23 формата ГАР)</summary>

| Формат | Ent-модель | Таблица |
|---|---|---|
| `AS_ADDR_OBJ` | `ent.AddrObj` | `gar.addr_obj` |
| `AS_HOUSES` | `ent.House` | `gar.houses` |
| `AS_STEADS` | `ent.Stead` | `gar.steads` |
| `AS_APARTMENTS` | `ent.Apartment` | `gar.apartments` |
| `AS_ROOMS` | `ent.Room` | `gar.rooms` |
| `AS_CARPLACES` | `ent.Carplace` | `gar.carplaces` |
| `AS_REESTR_OBJECTS` | `ent.ReestrObject` | `gar.reestr_objects` |
| `AS_ADM_HIERARCHY` | `ent.AdmHierarchyItem` | `gar.adm_hierarchy` |
| `AS_MUN_HIERARCHY` | `ent.MunHierarchyItem` | `gar.mun_hierarchy` |
| `AS_ADDR_OBJ_DIVISION` | `ent.AddrObjDivision` | `gar.addr_obj_division` |
| `AS_CHANGE_HISTORY` | `ent.ChangeHistory` | `gar.change_history` |
| `AS_PARAM` | `ent.Param` | `gar.param` |
| `AS_ADDR_OBJ_TYPES` | `ent.AddrObjType` | `gar.addr_obj_types` |
| `AS_APARTMENT_TYPES` | `ent.ApartmentType` | `gar.apartment_types` |
| `AS_HOUSE_TYPES` | `ent.HouseType` | `gar.house_types` |
| `AS_ADDHOUSE_TYPES` | `ent.AddHouseType` | `gar.add_house_types` |
| `AS_ROOM_TYPES` | `ent.RoomType` | `gar.room_types` |
| `AS_OPERATION_TYPES` | `ent.OperationType` | `gar.operation_types` |
| `AS_PARAM_TYPES` | `ent.ParamType` | `gar.param_types` |
| `AS_OBJECT_LEVELS` | `ent.ObjectLevel` | `gar.object_levels` |
| `AS_NORMATIVE_DOCS` | `ent.NormativeDoc` | `gar.normative_docs` |
| `AS_NORMATIVE_DOCS_KINDS` | `ent.NormativeDocKind` | `gar.normative_doc_kinds` |
| `AS_NORMATIVE_DOCS_TYPES` | `ent.NormativeDocType` | `gar.normative_doc_types` |

Особенности: идентификаторы `xs:long` — `int64`/`bigint`; GUID — `uuid`;
неограниченные `xs:integer` — `record.Integer` (каноническая строка, `numeric`);
`record.Date` — календарная дата XSD без перевода в UTC; `record.Flag` читает
`"1"/"0"` и `"true"/"false"`; ссылки между форматами — числовые поля без внешних
ключей (файлы можно грузить в любом порядке).

</details>

Документация форматов ГАР — соседняя папка `../../` (XSD: `../../unpacked/xsd`,
описания полей: `../../02 Форматы`, правила адресной строки:
`../../01 Руководства/Формирование адресной строки.md`).

Address-api operational endpoints `/metrics`, `/healthz`, `/readyz` use `ADDRESS_API_ADMIN_HTTP_ADDR` (default `:9214`), accessible inside the compose network without a host port mapping.

Suggester gRPC reflection is disabled by default; set `GRPC_REFLECTION=true` for development inspection. `CACHE_TTL` must be positive and at most `15m`.
