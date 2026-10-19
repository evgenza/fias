# gar-importer

Хранение файловой выгрузки [ГАР (ФИАС)](https://fias.nalog.ru/Frontend)
в PostgreSQL через [Ent](https://entgo.io/) и подготовка данных для адресного
поиска в OpenSearch или другом поисковом движке.

Документация форматов — соседняя папка `../` (XSD-схемы: `../unpacked/xsd`,
описания полей: `../02 Форматы`, правила построения адресной строки:
`../01 Руководства/Формирование адресной строки.md`).

## Структура

```
cmd/gar-importer    — entrypoint: сигналы и коды выхода; логика в internal/cli
cmd/gar-gen-spec    — генерация спецификаций атрибутов из закреплённых XSD
cmd/gar-gen-records — генерация независимых записей из моделей Ent
ent/schema          — исходные схемы 23 поддерживаемых типов файлов ГАР
ent/template        — генерация адаптеров records → пакетные операции Ent
ent/                — генерируемые модели, CRUD, запросы, транзакции и upsert
internal/cli        — разбор флагов, вывод, коды завершения (тестируемый Run)
internal/config     — env/.env, настройки пула соединений, маскирование DSN
internal/control    — журнал импорта (gar_control): запуски, файлы,
                      checkpoint, блокировка записи dataset
internal/gartype    — Date, Flag, Integer: XML/JSON и SQL-представление
internal/gar        — каталог форматов: Kind, имена файлов, регионы,
                      спецификации атрибутов (из xsd/), план архива
internal/gar/record — независимые типизированные записи ГАР и потоковый
                      декодер с валидацией; не зависит от ORM
internal/garxml     — потоковое декодирование XML-документа по записям
internal/importjob  — сценарий импорта: план, фильтры, пакетирование,
                      прогресс, отчёт (порты Archive и Writer)
internal/input/zipgar — источник: инвентаризация ZIP, открытие XML
internal/storage    — Ent-клиент, пул pgx, миграции и пакетный writer
migrations/         — SQL-план первоначальной миграции, сгенерированный Ent
xsd/                — закреплённые копии исходных XSD ФИАС (22 файла)
docs/               — каталог форматов, базовый анализ, журнал рефакторинга
compose.yaml        — PostgreSQL 18.6 Alpine с постоянным томом и healthcheck
```

Источник полей и XML/JSON-тегов — `ent/schema` (StructTag). Из них
`cmd/gar-gen-records` генерирует независимые записи `internal/gar/record`
(без зависимости от ORM): они используются потоковым декодером и адаптерами
записи, а Ent-модели остаются представлением хранения. Тест соответствия
не даёт записям и моделям разъехаться.

## Запуск PostgreSQL и миграция

Нужны Go 1.27.1+ и Docker Compose. Если `.env` ещё не создан:

```sh
cp .env.example .env
```

В `.env` задаются `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`,
`POSTGRES_PORT` и соответствующий `DATABASE_URL`. Значения по умолчанию —
база и пользователь `gar`, порт `5432`. Локальный `.env` исключён из Git.
При подготовке проекта создан `.env` со случайным паролем.

```sh
docker compose up -d --wait postgres
go run ./cmd/gar-importer migrate
```

Сервис слушает `127.0.0.1:5432`. Данные находятся в Docker-томе
`gar-importer_postgres_data`, подключённом к `/var/lib/postgresql` для PostgreSQL 18.
Переменная `DATABASE_URL` из окружения имеет приоритет над `.env`.

## Версионированные миграции

Схема хранится как версионированная история в формате Atlas: каталог
`migrations/atlas` (файлы `NNN_имя.up.sql` / `.down.sql` + контрольные суммы
`atlas.sum`). Команда `migrate` применяет ожидающие версии по одной в отдельной
транзакции вместе с записью в реестр ревизий (`gar_control.atlas_schema_revisions`)
под advisory-блокировкой: параллельный запуск ждёт и не дублирует работу.

```sh
go run ./cmd/gar-importer migrate            # применить ожидающие версии
go run ./cmd/gar-importer migrate --plan     # только показать ожидающие (не меняет базу)
```

Новая миграция схемы генерируется сравнением с одноразовой dev-базой
(целевая база не участвует и не изменяется):

```sh
# 1) применить текущий каталог к пустой dev-базе
psql "$DEV_URL" -c "CREATE SCHEMA gar"
go run ./cmd/gar-importer migrate  # на dev-базе через DATABASE_URL
# 2) сгенерировать следующий файл по отличию dev-базы от ent/schema
go run ./cmd/gar-migrate-diff -name add_new_format -dev-url "$DEV_URL"
```

`ent/schema` — источник желаемого состояния; ранняя история файлов не
редактируется задним числом. Проверка состояния существующей (legacy) базы
и оформление baseline: `migrate --baseline` — база проверяется по
фактическому содержимому против метаданных Ent; слепая пометка невозможна.
Процедура перехода заполненной legacy-базы: `migrations/legacy/README.md`.

## Генерация и работа с Ent

После изменения `ent/schema`:

```sh
go generate ./ent
go run ./cmd/gar-importer migrate
```

Генератор Ent закреплён в `go.mod` как Go tool. Зависимость `x/tools`
обновлена для совместимости генератора с Go 1.27.

```go
database, err := storage.Open(ctx, os.Getenv("DATABASE_URL"))
if err != nil {
    return err
}
defer database.Close()

objects, err := database.Client.AddrObj.Query().
    Where(addrobj.ObjectIDEQ(162142236)).All(ctx)
```

Для записи доступны генерируемые `Create`, `CreateBulk`, `Update`, `Delete`,
`OnConflict` и транзакции. Первичные ключи берутся из ГАР и не генерируются
PostgreSQL. Исключение — внутренний ключ `Param.ID`: исходный ID хранится в
`SourceID`, а уникальность задаётся парой `(source, source_id)` для раздельных
файлов параметров. Ссылки между форматами остаются числовыми полями без внешних ключей:
это сохраняет возможность загружать файлы в любом порядке.

## Модели ↔ форматы

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

Особенности моделей:

- `ADROBJECTID` и `OBJECTGUID` — строки GUID; числовые идентификаторы с
  типом `xs:long` — `int64` / PostgreSQL `bigint`. GUID хранятся как `uuid`.
- Ограниченные `xs:integer` до 10 цифр — `int64`, независимо от архитектуры
  Go. Поля с 19 цифрами в `AS_STEADS` и неограниченные `xs:integer` —
  `record.Integer`, каноническая десятичная строка без потери цифр.
  В PostgreSQL это `numeric(19,0)` для участков и `numeric` для остальных
  полей с неограниченным диапазоном.
  Для арифметики доступен `Integer.Int64()` с явной ошибкой переполнения.
- `ADDR_OBJ.LEVEL` и `STEADS.OPERTYPEID` — строки, как в XSD;
  ведущие нули сохраняются. Коды регионов и ОКТМО также остаются строками.
- `record.Date` — строковая календарная дата `xs:date`. При чтении допускаются
  `Z` и смещение часового пояса; сохраняется исходный календарный день без
  перевода в UTC. Поддерживаются отрицательные и расширенные годы XSD 1.0.
  `Date.Time()` возвращает `(time.Time, error)` для годов 1..9999.
  В PostgreSQL используется `date`; допустимый диапазон ограничен самим PostgreSQL.
- `record.Flag` читает `"1"/"0"` и `"true"/"false"`; JSON содержит boolean.
- Необязательные атрибуты XSD — указатели. В JSON незаданные поля опускаются.
- JSON-поля имеют имена `snake_case`; идентификаторы передаются строками,
  чтобы сохранить точность при передаче через JSON и хранении как `keyword`.
  Это представление исходных записей, а не готовый поисковый документ.
- Ent называет любой первичный ключ `ID`: у `ReestrObject.ID` это атрибут
  XML `OBJECTID` и SQL `object_id`, у `ChangeHistory.ID` — `CHANGEID` / `change_id`,
  у `ObjectLevel.ID` — `LEVEL` / `level`. Имена XML/JSON и столбцов сохранены.
- `Param.SourceID` содержит XML `ID`; `Param.Source` — нормализованный
  префикс файла (`AS_ADDR_OBJ_PARAMS`, `AS_HOUSES_PARAMS` и т. д.).
  Суффиксы `PARAM` и `PARAMS` поддерживаются. Все шесть семейств параметров
  хранятся в `gar.param` без пересечения их идентификаторов.
- Документация описывает 22 формата. В реальном ZIP дополнительно есть
  `AS_ADDHOUSE_TYPES`, использующий структуру `HOUSETYPES/HOUSETYPE`.
  Дополнительные типы домов хранятся отдельно от `AS_HOUSE_TYPES`.

## Локальный импорт ZIP

```sh
go run ./cmd/gar-importer import --zip "$HOME/Downloads/gar_xml.zip"
```

Продолжение прерванного запуска (по digest архива; завершённые файлы
пропускаются, незавершённый файл продолжается с подтверждённого checkpoint):

```sh
go run ./cmd/gar-importer import --zip "$HOME/Downloads/gar_xml.zip" --resume
```

Пробный запуск по 200 записей каждого XML-файла:

```sh
go run ./cmd/gar-importer import --zip "$HOME/Downloads/gar_xml.zip" \
  --limit-per-file 200 --batch-size 200
```

Выбор регионов и форматов:

```sh
go run ./cmd/gar-importer import --zip "$HOME/Downloads/gar_xml.zip" \
  --regions 77,50 --formats AS_HOUSES,AS_ADDR_OBJ --batch-size 1000
```

`--regions` включает также общероссийские справочники из корня архива.
`--formats AS_PARAM` выбирает файлы параметров всех шести типов объектов;
`--formats AS_HOUSES_PARAMS` — только параметры домов (точное семейство).
Поддерживаются обёртки `gar_xml/` и `gar_delta_xml/` первым сегментом пути.
Без фильтров и `--limit-per-file` читаются все поддерживаемые XML целиком.
Пакет по умолчанию — 1000 записей, допустимый размер — 1..2000.

Программный метод: сценарий отделён от Ent и ZIP через порты.

```go
archive, _ := zipgar.Open(zipPath)
database, _ := storage.Open(ctx, cfg)
report, err := importjob.Run(ctx, archive, database, importjob.Options{
    BatchSize:    1000,
    Regions:      []int{77},
    LimitPerFile: 200, // 0 для полного импорта
})
```

Сценарий читает ZIP без распаковки на диск и декодирует XML по одной записи.
Память зависит от размера пакета. Поддерживаются UTF-8 и Windows-1251.
Генерируемые адаптеры из `ent/template` строят Ent-builders из типизированных
записей без reflection на каждой записи. Подсчёт счётчиков не увеличивается
до успешного коммита пакета; `записано` — дедуплицированные операции записи.

Пакеты сохраняются через `CreateBulk` и upsert. Повторный импорт обновляет
записи, включая очистку исчезнувших необязательных значений до SQL NULL.
При повторении ID в одном пакете сохраняется последняя запись.
Счётчик `записано` показывает обработанные операции записи, включая обновления.

При ошибке или Ctrl+C уже сохранённые пакеты остаются в БД; неполный
текущий пакет не записывается. Можно повторить запуск с теми же параметрами.
Ошибка включает имя файла, номер записи и позицию XML. Записи проверяются
по спецификациям из закреплённых XSD до записи: пропущенный обязательный
атрибут, пустая дата, неверная длина или неизвестный атрибут прерывают
импорт с указанием атрибута и формата.

Не поддерживаемые файлы всегда перечисляются как пропущенные с причиной:
неизвестный формат, повреждённое имя известного формата, неожиданная папка
(не регион и не корень), голый `AS_PARAM` без семейства объектов, повторный
путь в архиве. Файлы без расширения XML игнорируются. Режим с лимитом читает
только начало каждого XML; при полном чтении проверяется также CRC ZIP.

## Подготовка к поиску

Дальнейший этап — сборка полного адреса по `PATH` и отправка документов пакетами
Bulk в поисковый движок. Для актуального поиска учитываются `ISACTUAL` и `ISACTIVE`.

При чтении XML записи проверяются по спецификациям из `xsd/` (см.
`internal/gar`): обязательность и ограничения атрибутов заданы закреплёнными
XSD ФИАС, спецификации генерируются `cmd/gar-gen-spec`.

JSON идентификаторов следует отображать в `keyword`, признаки — в `boolean`,
обычные календарные даты — в `date`, поля адресного поиска — в `text`.
Для отрицательных/расширенных лет потребуется отдельное отображение в целевой
движок: способность XML-модели прочитать значение не означает, что любой
движок поддерживает его в своём типе даты. Модели и разбор файлов не зависят
от конкретного поискового движка.

Структура PostgreSQL не определяет структуру поискового индекса.
Методы `Scan/Value` пользовательских типов используются Ent через `database/sql`.

## Сборка

```sh
go build ./...
go test ./...
go vet ./...
```

Интеграционные тесты запускаются отдельно на пустой тестовой базе, заданной
через `GAR_TEST_DATABASE_URL` (никогда не обычный `DATABASE_URL`). Проверяются
все модели, длинные числа, GUID, SQL NULL, повторный импорт ZIP, фильтры,
отмена и ошибочный XML. Тесты пишут реальные данные: перед запуском сбрасывают
схему `gar`, поэтому пакеты прогоняются последовательно (`-p 1`). Если
переменная не задана, эти тесты пропускаются:

```sh
go test -tags=integration -p 1 ./internal/... -v
```
