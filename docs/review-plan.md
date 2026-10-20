# План исправлений по код-ревью (2026-10-20)

Итог ревью всего репозитория: импортёр, индексатор и сборка адресов, address-api и
suggester, инфраструктура и CI. Находки сверены с кодом и с документацией ГАР
(`../../docs`: «Формирование адресной строки», «Получение обновлений», форматы, XSD).
Пункты с пометкой **«проверить»** опираются на поведение внешних систем по памяти —
агент сначала подтверждает проблему и только потом чинит.

## Как работать с планом (инструкция для агента)

1. Берите **одну задачу** (`A1`, `C4`…). Проверьте раздел «Зависимости»: если там
   есть незакрытые задачи, возьмите другую.
2. Убедитесь, что проблема воспроизводится в текущем коде (строки могли сдвинуться —
   ищите по смыслу). Если не воспроизводится — отметьте задачу `[~]` с пояснением
   и остановитесь.
3. Сначала тест, который падает на текущем коде, затем исправление.
4. Сгенерированный код не правится руками: `ent/` — через `ent/schema` и
   `ent/template` + `go generate ./ent`; `internal/gar/record/*_gen.go`,
   `internal/garxml/spec_gen.go` — `go generate .`; proto — `make proto`.
5. Перед сдачей:
   ```sh
   go build ./... && make vet && golangci-lint run ./... && go test ./...
   make test-integration   # если задача трогает БД/NATS/ES/Redis/MinIO (нужен Docker)
   ```
6. Отметьте задачу `[x]`, добавьте одну строку в «Журнал выполнения» в конце
   файла, запишите крупные изменения поведения в `docs/refactoring-log.md`.
7. Один коммит (или PR) на задачу; сообщение коммита начинается с ID задачи.

**Параллельная работа.** У каждой задачи указаны файлы. Две задачи, у которых
файлы пересекаются, нельзя давать параллельным агентам. Особенно часто
пересекаются `internal/events/subscriber.go`, `internal/worker/worker.go`,
`services/indexer/internal/app/app.go`, `internal/address/builder.go`,
`services/address-api/internal/app/*`, `compose.yaml`.

**Статусы:** `[ ]` — не начата, `[~]` — не подтвердилась или отложена (с причиной),
`[x]` — выполнена.

## Порядок выполнения

| Волна | Задачи | Смысл |
|---|---|---|
| 0 — быстрые | E1, E2, E3, A3, D1, D3 | дешёвые исправления с большим эффектом |
| 1 — корректность конвейера | A1 → A2, B1, B2, C1, C4 | дубли, потери событий, поиск |
| 2 — данные и порядок | B3 → B4, C2 → C3, C6 | целостность данных и производительность |
| 3 — правила адресов | C7 | соответствие документации |
| 4 — устойчивость API | D2, D4, D5, D6, D7, D8, D9, D10 | нагрузка и отказы |
| 5 — инфраструктура и CI | E4…E10 | безопасность и автоматические проверки |
| 6 — мелочи | B5…B9, C5, C8, E11 | остальное |

---

## A. NATS и обработка событий (общее для importer и indexer)

### [ ] A1. Повторная доставка во время долгой обработки — Critical
- **Файлы:** `internal/events/subscriber.go` (consumer config ~110–118, `fetchBatch`
  ~18, `fetchLoop` ~136–174), `internal/worker/worker.go` (`handle` ~221–269),
  `services/indexer/internal/app/app.go` (~154–158, 206–221).
- **Проблема:** `InProgress()` нигде не вызывается. AckWait 30 с при одновременно
  заданном `BackOff` — **проверить**, не подменяет ли nats-server AckWait на
  `BackOff[0]` = 1 с. Импорт и пересборка региона длятся минуты или часы. `fetchLoop` сразу
  делает следующий `Fetch(16)` и получает повторную доставку того же сообщения:
  импорт архива запускается второй раз, регион пересобирается второй раз. Каждая
  неудачная попытка съедает примерно две доставки из `MaxDeliver=10`.
- **Исправление:**
  - пока работает обработчик, вызывать `msg.InProgress()` из отдельной горутины
    с интервалом AckWait/2;
  - сделать AckWait явным и не зависящим от BackOff (убрать `BackOff` из конфига
    и делать задержку повторов через `NakWithDelay`);
  - не выбирать новые сообщения, пока обработчик занят (`Fetch(1)` или буфер
    канала, равный размеру пачки).
- **Готово, когда:** интеграционный тест с обработчиком дольше AckWait получает
  сообщение ровно один раз; конфигурация consumer обновляется у существующего
  durable.

### [ ] A2. Сообщения теряются молча после MaxDeliver — High
- **Зависимости:** A1.
- **Файлы:** `internal/events/subscriber.go`, `internal/events/bus.go` (~43–49),
  `internal/worker/worker.go`, `services/indexer/internal/app/app.go`.
- **Проблема:**
  - после `MaxDeliver` событие или задача импорта пропадают: DLQ нет, advisory
    `MAX_DELIVERIES` никто не слушает;
  - битые payload'ы терминируются без лога и метрики (`subscriber.go` ~162–165);
  - у потока нет `MaxAge`/`MaxBytes`, а новый durable с `DeliverAllPolicy`
    переиграет всю историю.
- **Исправление:**
  - при `NumDelivered >= MaxDeliver` делать `Term()` и публиковать сообщение
    в `gar.dlq.<subject>`; в worker дополнительно ставить run в `failed` с причиной;
  - логировать и считать `Term()` для битых payload'ов;
  - задать лимиты потока;
  - решить, нужен ли `DeliverNewPolicy` для новых durable индексатора.
- **Готово, когда:** тест с постоянно падающим обработчиком видит сообщение в DLQ,
  в логе и в метрике.

### [ ] A3. Детерминированные ошибки данных считаются временными — Medium
- **Файлы:** `internal/worker/worker.go` (`isPermanent` ~352–375),
  `internal/gar/validate.go` (~129–138), генератор `cmd/gar-gen-spec`,
  `internal/garxml/stream.go` (~64, 70), `internal/importjob/job.go` (~265).
- **Проблема:** следующие ошибки не являются `RecordError`, поэтому сообщение
  получает Nak и 10 повторов с полной перезагрузкой архива:
  - переполнение полей xs:integer, которые в Go имеют тип int64:
    - `Room.OpertypeID`, `Room.RoomType`;
    - `Apartment.ApartType`;
    - `Carplace.OpertypeID`;
    - `ChangeHistory.OpertypeID`;
    - `NormativeDoc.Type`, `NormativeDoc.Kind`;
    - `ObjectLevel.LEVEL`;
    - `Param.TypeID`;
    - ID справочников типов;
  - `ISACTUAL`/`ISACTIVE` не из перечисления 0/1;
  - неверный корневой элемент XML;
  - «в архиве нет выбранных XML-файлов»;
  - повреждённый ZIP (`io.ErrUnexpectedEOF`, `flate.CorruptInputError`);
  - OBJECTGUID, который не является UUID;
  - SQLSTATE класса 22 (ошибки данных).
- **Исправление:**
  - генерировать проверку диапазона по реальному Go-типу поля и проверку
    перечислений из XSD;
  - оборачивать ошибки декодирования в `RecordError`;
  - считать постоянными SQLSTATE 22xxx/23xxx и sentinel-ошибки сценария;
  - проверять OBJECTGUID на формат UUID (в БД колонка `uuid`).
- **Готово, когда:** табличный тест `isPermanent` покрывает все перечисленные
  случаи; `go generate . -verify` чистый.

---

## B. Импортёр

### [ ] B1. Checkpoint пишет размер пакета вместо накопленного счётчика — High
- **Файлы:** `ent/template/gar_batch.tmpl` (~139–152) → `go generate ./ent`;
  `internal/storage/postgres.go` (~80–92); `internal/control/control.go`
  (~208–211); тест `internal/importjob/zip_test.go` (~240).
- **Проблема:** `b.checkpoint(ctx, drv, b.received)` получает число записей
  с последнего коммита, а `received` после коммита обнуляется. В итоге
  `last_committed_record` навсегда остаётся около `base + batchSize`, и resume
  внутри файла пропускает только первый пакет. Кроме того, счётчики `committed`,
  `writeOps` и `onCommit` обновляются до `tx.Commit()`.
- **Исправление:**
  - передавать `b.committed + b.received`;
  - двигать счётчики и вызывать `onCommit` только после успешного Commit
    (перенести в вызывающий код);
  - тип `decoded` в `control.go` (~91, 230) — `int64`.
- **Готово, когда:** тест после N пакетов проверяет
  `last_committed_record == N*batch` и что resume пропускает ровно столько записей.

### [ ] B2. Resume не находит прерванные запуски, worker никогда не возобновляет — High
- **Файлы:** `internal/control/control.go` (`LatestResumableRun` ~274–285),
  `internal/importjob/job.go` (~204–216, 287–299), `internal/worker/worker.go` (~293–298).
- **Проблема:**
  - ищутся только запуски `loading`/`planned`, хотя при ошибке ставится `failed`,
    а при отмене — `canceled`;
  - worker всегда стартует новый run;
  - при resume не сверяются Regions/Kinds/Sources исходного run.
- **Исправление:**
  - искать `failed`/`canceled`/`loading`/`planned`;
  - в worker использовать `ResumeRunID = -1` с fallback на новый run;
  - при расхождении параметров отказывать в resume;
  - `Options.SchemaName` либо применять и к Ent (search_path), либо убрать.
- **Готово, когда:** интеграционный тест «Ctrl-C → `--resume`» продолжает тот же
  run и не перезаписывает завершённые файлы.

### [ ] B3. Идемпотентность задачи импорта — High
- **Зависимости:** A1, B2.
- **Файлы:** `internal/worker/worker.go`, `internal/control/control.go`.
- **Проблема:** повторная задача с тем же архивом заново скачивает и импортирует
  его и публикует второй комплект `gar.objects.changed` с другими Msg-Id.
- **Исправление:** если для того же digest уже есть run в состоянии `ready` —
  сразу Ack с логом; Msg-Id событий строить от digest, а не от runID.

### [ ] B4. Нет контроля порядка дельт, upsert безусловный — Critical
- **Зависимости:** B3. Перед кодом нужен короткий дизайн в
  `docs/decisions.md`.
- **Файлы:** `ent/template/gar_batch.tmpl` (upsert), `internal/control/control.go`
  (~63, 72–73: `applied_release`, `base_release`, `target_release`, `entry_date`
  пишутся, но не проверяются), `internal/importjob/job.go` (~273–276: режим всегда
  `snapshot`), `internal/worker/worker.go` (~264: Nak с задержкой пропускает
  следующую задачу вперёд).
- **Проблема:** по «Получение обновлений.md» дельты применяются строго по порядку
  версий. Сейчас повторно применённая старая дельта или старый полный архив
  перезаписывает новые данные, например снова выставляет `ISACTUAL=1` версии,
  которую новая дельта закрыла.
- **Исправление:**
  - различать полную выгрузку и дельту, определять версию архива (дата
    в именах файлов или `version.txt`);
  - хранить `datasets.applied_release` и отклонять или откладывать архивы старее
    применённого или с пропуском версии;
  - обрабатывать задачи импорта строго последовательно (при временной ошибке
    не брать следующую);
  - добавить в upsert защиту `WHERE excluded.update_date >= t.update_date`
    (или по `change_id`), где поле есть.
- **Готово, когда:** тесты «D2 затем D1 → D1 отклонена» и «старая полная выгрузка
  поверх новой → отказ».

### [ ] B5. Ненадёжная advisory-блокировка — Medium
- **Файлы:** `internal/control/control.go` (~303–324).
- **Проблема:**
  - если соединение с блокировкой рвётся, она снимается молча, а импорт
    продолжает писать — возможна параллельная запись двух процессов;
  - если `pg_advisory_unlock` не прошёл, соединение возвращается в пул
    с удерживаемой блокировкой.
- **Исправление:**
  - периодически пинговать lock-соединение и при потере отменять ctx импорта;
  - если unlock не прошёл, закрывать физическое соединение (`conn.Raw` → Close).

### [ ] B6. Известные файлы с повреждённым именем пропускаются молча — Medium
- **Файлы:** `internal/importjob/job.go` (~230–232), `internal/gar/plan.go` (~60–83).
- **Проблема:**
  - `AS_HOUSES_2024011_x.XML` или папка `100/` попадают только в `report.Skipped`,
    а run всё равно получает `ready`;
  - при дубликатах путей в ZIP `zipgar` берёт последнюю запись, а `Plan` — первую
    (`internal/input/zipgar/zip.go` ~36–37).
- **Исправление:**
  - при `NameError.Known` или неизвестной папке региона — ошибка run (или
    явный флаг `--allow-skip`);
  - дубликат пути в ZIP — ошибка.

### [ ] B7. `gar-migrate-diff` и формат миграций — Medium (проверить)
- **Файлы:** `cmd/gar-migrate-diff/main.go` (~63–72), `internal/migrations/runner.go` (~85–87).
- **Проблема:** **проверить**, генерирует ли diff без `WithFormatter` файлы `.sql`,
  тогда как раннер читает только `*.up.sql`. Кроме того, diff требует
  `DATABASE_URL`, хотя он не нужен.
- **Исправление:** `sqltool.GolangMigrateFormatter`; не требовать `DATABASE_URL`.
- **Готово, когда:** сгенерированная миграция подхватывается раннером (тест).

### [ ] B8. Мелочи импортёра — Low
- **Файлы:** `internal/objectstorage/client.go` (~43–50, 124–126),
  `internal/importjob/job.go` (~162), `internal/worker/worker.go` (~326–327).
- **Что сделать:**
  - удалять tempdir `gar-object-*` после импорта и при ошибке Download;
  - убрать двойное закрытие архива;
  - `filesTotal{done}` и публикацию события делать после успешного `FinishFile`.

### [ ] B9. Ключ `change_history` — Low (проверить)
- **Файлы:** миграция `migrations/atlas/*`, `ent/schema/changehistory.go`.
- **Проблема:** PK — `change_id`. **Проверить** по данным и XSD, уникален ли
  CHANGEID на строку: если одна транзакция затрагивает несколько объектов,
  строки перезаписывают друг друга.
- **Исправление:** при подтверждении — составной ключ (`change_id`, `object_id`,
  `adr_object_id`) и миграция.

---

## C. Индексатор, сборка адресов, Elasticsearch

### [ ] C1. Справочники типов: гонка данных и вечный кэш — High
- **Файлы:** `internal/address/builder.go` (`dictionaries` ~465–485),
  `internal/address/describe.go` (~224, 285), `services/indexer/internal/app/app.go`
  (~177–184: события региона 0 подтверждаются без действий).
- **Проблема:**
  - `b.dicts` читается и пишется без синхронизации, а Builder общий для
    параллельных HTTP-запросов address-api;
  - пустые справочники (если сервис стартовал до импорта) кэшируются навсегда:
    у всех домов тип «д», дополнительные типы пропадают;
  - новые типы из дельты не видны до рестарта.
- **Исправление:**
  - `atomic.Pointer` + `singleflight` (или мьютекс);
  - не кэшировать пустой результат;
  - TTL и/или сброс по событиям `*_TYPES` (регион 0) в indexer.
- **Готово, когда:** `go test -race` с параллельными `DescribeObject` чистый;
  тест «пустые справочники → импорт справочников → типы появились».

### [ ] C2. Пересборка региона сканирует таблицы всей страны — Critical
- **Файлы:** `internal/address/builder.go` (~26–91, 294–318), новая миграция
  индексов (`migrations/atlas`, `ent/schema`).
- **Проблема:**
  - подзапросы `SELECT DISTINCT ON (object_id) … FROM gar.houses ORDER BY …`
    (а также addr_obj, apartments, rooms, steads, carplaces) не фильтруются
    по региону;
  - `region_code::int = $1` выполняется без индекса;
  - при полном импорте получается порядка 1500 полных сортировок десятков
    миллионов строк;
  - курсор открыт на всё время bulk в ES и мешает VACUUM.
- **Исправление:**
  - сначала выбирать object_id региона из `adm_hierarchy` по индексу
    (`region_code` или функциональный индекс);
  - затем `JOIN LATERAL (… WHERE object_id = h.object_id ORDER BY … LIMIT 1)`
    по индексу `(object_id, is_active, is_actual, end_date, id)`;
  - читать постранично по keyset, а не одним курсором;
  - заодно `region_code`: `CASE WHEN … ~ '^[0-9]+$' THEN region_code::int END`
    (~28).
- **Готово, когда:** `EXPLAIN` на фикстуре без Seq Scan по большим таблицам;
  результат индексации на фикстуре не изменился (golden-тест).

### [ ] C3. Маршрутизация событий: изменения предков не доходят до потомков — High
- **Зависимости:** C2 (иначе каскад умножит полные сканы), A1.
- **Файлы:** `services/indexer/internal/app/app.go` (~238–256).
- **Проблема:**
  - `AS_ADDR_OBJ` пересобирает только addr_obj: переименование улицы
    не обновляет дома и квартиры;
  - `AS_HOUSES` не пересобирает apartments/rooms;
  - `AS_REESTR_OBJECTS` игнорируется, поэтому при первичном импорте у домов
    и квартир `level=0`;
  - `AS_MUN_HIERARCHY` запускает 6 пересборок впустую: строки строятся по ADM;
  - `AS_PARAM` игнорируется (нужно для C7).
- **Исправление:**
  - пересобирать регион один раз на (`run_id`, регион), когда в журнале
    завершены все файлы региона (дебаунс);
  - либо таблица зависимостей: ADDR_OBJ/REESTR → всё;
    HOUSES → houses, apartments, rooms, carplaces; APARTMENTS → apartments, rooms;
  - MUN — без действий, пока не используется.
- **Готово, когда:** интеграционный тест «переименовали улицу дельтой → документ
  дома в ES с новым названием»; у домов после первичного импорта правильный `level`.

### [ ] C4. Маппинг ES не находит короткие и длинные токены — High
- **Файлы:** `internal/search/search.go` (~245–268),
  `services/suggester/internal/server/server.go` (~172–173).
- **Проблема:**
  - `edge_ngram min_gram=2, max_gram=15`, а у search-анализатора нет обрезки;
  - при `operator: and` запросы «тверская 7», «г москва», «краснопресненская»
    дают 0 результатов;
  - анализатор `russian` выкидывает «к», «с» как стоп-слова;
  - нет нормализации ё→е.
- **Исправление:**
  - `min_gram: 1`, `max_gram` 20 + `truncate` в search-анализаторе (либо
    `search_as_you_type` / `match_bool_prefix`);
  - char_filter ё→е в обоих анализаторах;
  - стоп-слова для адресных полей отключить;
  - миграция индекса через алиас: создать `gar-addresses-v2`, переиндексировать,
    переключить алиас;
  - создавать индекс через index template или `require_alias`, чтобы автосоздание
    с динамическим маппингом было невозможно (было L3).
- **Готово, когда:** интеграционный тест suggester на запросы «тверская 7»,
  «г москва», «краснопресненская», «ёлочная»/«елочная».

### [ ] C5. Ошибки bulk и таймауты ES — Medium
- **Файлы:** `internal/search/search.go` (~91–98, 196–216),
  `services/indexer/internal/app/app.go` (~263–266).
- **Проблема:**
  - поэлементные 429/5xx валят всю пачку, и регион пересобирается с нуля;
  - метрика `documents` считает документы повторно;
  - у клиента ES нет таймаутов: зависшее соединение блокирует indexer навсегда.
- **Исправление:**
  - повторять только элементы с 429/5xx с backoff, ошибку возвращать
    только для неустранимых;
  - `ResponseHeaderTimeout` и контекст с дедлайном на bulk.

### [ ] C6. Устаревшие документы и запись из чужого региона — Medium
- **Файлы:** `internal/address/builder.go` (~26–29), `internal/search/search.go`.
- **Проблема:**
  - документы объектов, ушедших из региона или деактивированных, не удаляются;
  - `DISTINCT ON` выбирает запись иерархии внутри фильтра по региону без
    требования `is_active`, поэтому историческая запись может перезаписать документ
    старым PATH.
- **Исправление:**
  - сначала одна действующая запись иерархии на объект, затем фильтр по региону;
  - после пересборки `delete_by_query` по `region` и `indexed_at < start`.

### [ ] C7. Формирование адресной строки по правилам из docs — Medium
- **Зависимости:** C1; для пункта о параметре 16 — C3 (маршрутизация `AS_PARAM`).
- **Файлы:** `internal/address/format.go` (~22, 66–68, 92–107),
  `internal/address/builder.go` (~26–29, 39, 159, 205), тесты `format_test.go`.
- **Проблема** (по «Формирование адресной строки.md» и `AS_HOUSES`, `AS_ADM_HIERARCHY`):
  1. Дом без HOUSENUM получает выдуманный тип «д» («д к 1» вместо «к 1»), и `Name`
     пустой.
  2. Для уровней 1, 3, 4 не используется официальное наименование (PARAM TYPEID=16):
     получается «обл Московская» вместо «Московская область».
  3. Объекты без REGIONCODE (поле необязательное) или существующие только
     в MUN-иерархии не попадают в индекс.
  4. Тип участка «уч» зашит в код, а по правилам его надо брать из
     `ADDR_OBJ_TYPES` LEVEL=9.
- **Исправление:**
  1. Основной тип подставлять только при непустом номере; `Name` брать из первого
     непустого номера.
  2. Подгружать действующий param 16 для предков уровней 1, 3, 4.
  3. Регион брать из корня PATH; объекты только из MUN индексировать по MUN.
  4. Тип участка брать из справочника.
- **Готово, когда:** golden-тесты на примеры из «Формирование адресной строки.md».

### [ ] C8. Мелочи индексатора — Low
- **Файлы:** `services/indexer/internal/app/app.go` (~179, 198).
- **Что сделать:** метку `kind` из payload пропускать через белый список
  (иначе `"unknown"`).

---

## D. address-api и suggester

### [ ] D1. Readiness: IDLE gRPC и жёсткие зависимости — High
- **Файлы:** `services/address-api/internal/app/app.go` (~215–231),
  `internal/platform/http.go` (~75).
- **Проблема:**
  - через 30 минут простоя gRPC-канал уходит в IDLE, `GetState()` его не будит,
    и `/readyz` отвечает 503 навсегда;
  - при недоступности Redis или suggester все реплики выпадают из балансировки,
    хотя карточки работают из PostgreSQL;
  - текст ошибки (адреса, пользователь БД) уходит клиенту.
- **Исправление:**
  - в readiness оставить только PostgreSQL;
  - при IDLE вызывать `Connect()`;
  - состояние Redis и suggester отдавать в метриках;
  - тело ответа — просто «not ready», детали писать в лог.

### [ ] D2. Таймауты HTTP-сервера и запросов — Medium
- **Файлы:** `internal/platform/http.go` (~27–32, 50),
  `services/address-api/internal/app/app.go`.
- **Проблема:** задан только `ReadHeaderTimeout`: возможны Slowloris по телу и
  бесконечные keep-alive/h2c соединения. Таймаута на обработчик нет. Graceful
  shutdown без фазы дренажа; при таймауте `Shutdown` зависимости закрываются под
  живыми обработчиками; shutdown трейсинга без таймаута (address-api `app.go` ~48,
  suggester `app.go` ~42).
- **Исправление:**
  - `ReadTimeout` 15 с, `WriteTimeout` 30 с, `IdleTimeout` 120 с,
    `MaxHeaderBytes` 64 КБ;
  - `context.WithTimeout` на API-обработчики;
  - перед `Shutdown` переводить readyz в 503 и выдерживать паузу;
  - таймаут на shutdown трейсинга.

### [ ] D3. Кардинальность метрик и трейсов — High
- **Файлы:** `internal/platform/metrics.go` (~84–88),
  `services/address-api/internal/app/app.go` (~100–102), `internal/platform/tracing.go` (~29).
- **Проблема:**
  - метка `method` пишется сырой: каждый выдуманный HTTP-метод создаёт новые серии;
  - имя спана — сырой путь (каждый object_id — своё имя);
  - `ParentBased`-сэмплер доверяет внешнему `traceparent`.
- **Исправление:**
  - белый список методов (иначе `OTHER`);
  - имя спана по `r.Pattern`;
  - на публичной границе не доверять входящему sampled-флагу и фильтровать
    baggage.

### [ ] D4. N+1 в `/children` — High
- **Зависимости:** C1 (те же файлы `describe.go`/`builder.go`).
- **Файлы:** `services/address-api/internal/app/handlers.go` (~187–211),
  `internal/address/describe.go` (~109–152, 195–259),
  `services/address-api/static/web/app.js` (~174).
- **Проблема:** на каждого ребёнка вызывается `DescribeObject` — около 11 запросов,
  при `limit=200` получается около 2200 последовательных запросов. Пул — 8 соединений.
  Полное описание родителя выбрасывается. Если у родителя есть иерархия, но нет
  собственной записи, ответ 404, хотя дети есть.
- **Исправление:**
  - определять вид объекта по `reestr_objects.level_id`;
  - описывать детей пачкой с одной загрузкой предков;
  - проверку родителя делать через `EXISTS`;
  - лимит по умолчанию в веб-клиенте — 50.
- **Готово, когда:** число SQL на запрос `/children?limit=200` ≤ 10 (тест со счётчиком).

### [ ] D5. Валидация ввода и коды ошибок — Medium
- **Файлы:** `services/address-api/internal/app/handlers.go` (~87, 120–123, 251),
  `services/suggester/internal/server/server.go` (~73–75, 115, 140, 148–163, 228–231),
  `services/suggester/internal/server/metrics.go` (~43, 53).
- **Проблема:**
  - длина `q` не ограничена (до ~1 МБ);
  - ES 4xx превращается в `Unavailable` → HTTP 503 с ERROR-логом, в котором
    фрагмент запроса;
  - `region` не валидируется;
  - отмена клиентом даёт 500/ERROR/`Unknown` в метриках.
- **Исправление:**
  - ограничить `q` (200 рун или 20 токенов) → 400/InvalidArgument;
  - `region` — до 3 цифр;
  - ES 4xx → InvalidArgument/Internal;
  - `context.Canceled`/`DeadlineExceeded` → 499/504 на уровне Info,
    `status.FromContextError` в интерцепторе.

### [ ] D6. Кэш Redis — Medium
- **Файлы:** `services/address-api/internal/app/cache.go`, `app.go` (~66–69),
  `config.go` (~49–55), `handlers.go` (~171).
- **Проблема:**
  - таймауты Redis дефолтные (секунды), с ретраями: при недоступности Redis каждая
    карточка тормозит;
  - инвалидации после импорта нет, TTL не ограничен сверху;
  - `set` идёт с контекстом запроса и отменяется при уходе клиента;
  - нет негативного кэша и защиты от stampede.
- **Исправление:**
  - таймауты 100–300 мс, `MaxRetries` 0–1;
  - версия данных в ключе (ID последнего импорта из БД) или событие инвалидации;
  - ограничить TTL сверху;
  - `set` через `context.WithoutCancel` с коротким таймаутом;
  - негативный TTL 30 с и `singleflight` по objectID.

### [ ] D7. Служебные эндпоинты на публичном порту — Medium
- **Файлы:** `services/address-api/internal/app/app.go` (~115), `deploy/prometheus.yml` (~45),
  `compose.yaml`.
- **Исправление:** `/metrics` (и readyz/healthz) — на отдельный внутренний порт,
  как у suggester (`:9213`); обновить prometheus.yml и healthcheck в compose.

### [ ] D8. gRPC: клиент и сервер — Medium
- **Файлы:** `services/address-api/internal/app/app.go` (~75–83),
  `services/suggester/internal/app/app.go` (~52–62).
- **Проблема:**
  - у клиента нет keepalive и балансировки (`pick_first`);
  - health сервера всегда SERVING и не зависит от ES;
  - reflection включён без условий;
  - нет `MaxConcurrentStreams` и keepalive enforcement.
- **Исправление:**
  - keepalive (30 с / 10 с);
  - `round_robin` через `dns:///`;
  - retry policy для Unavailable;
  - health по ES и NOT_SERVING при остановке;
  - reflection за флагом;
  - лимиты на сервере.

### [ ] D9. Гонки ответов в веб-клиенте — Medium
- **Файлы:** `services/address-api/static/web/app.js` (~126–141, 173–209).
- **Исправление:**
  - общий `detailsSeq` для `showDetails`/`loadChildren`;
  - при коротком вводе увеличивать `requestSeq` или использовать `AbortController`.

### [ ] D10. OpenAPI и заголовки безопасности — Low
- **Файлы:** `api/openapi/address-api.yaml` (+ копия `services/address-api/static/openapi.yaml`),
  `services/address-api/internal/app/*`.
- **Что сделать:**
  - 404 и 405 от mux отдавать в формате `Error` и описать в спеке;
  - `ObjectID` `minimum: 1`;
  - пример `/readyz` привести к фактическому ответу;
  - добавить заголовки CSP, `X-Content-Type-Options: nosniff`, `frame-ancestors`.

---

## E. Инфраструктура, скрипты, CI

### [x] E1. Healthcheck Elasticsearch всегда успешен — High
- **Файлы:** `compose.yaml` (~38).
- **Проблема:** неэкранированный `&` в CMD-SHELL отправляет `curl` в фон, проверка
  всегда возвращает 0. Все `depends_on: elasticsearch: service_healthy`, `--wait`
  в CI и `wait_healthy` в chaos-скриптах ничего не ждут.
- **Исправление:** взять URL в одинарные кавычки внутри shell-строки.
- **Готово, когда:** при остановленном ES контейнер переходит в `unhealthy`.

### [x] E2. Ложный PASS в smoke-проверке Prometheus — Medium
- **Файлы:** `scripts/smoke.sh` (~64).
- **Исправление:** проверять через
  `jq -e '.data.activeTargets[] | select(.labels.job==$j and .health=="up")'`.

### [ ] E3. Гигиена репозитория — Low
- **Что сделать:**
  - `git rm -r --cached .idea` и добавить `.idea/` в `.gitignore`;
  - в `.env.example` вместо правдоподобных паролей — плейсхолдеры `change_me`;
  - `DATABASE_URL` с `${POSTGRES_PORT}`.

### [ ] E4. `make up --wait` и одноразовые контейнеры — Medium (проверить)
- **Файлы:** `Makefile` (~7), README (~63), `docs/report/demo.md` (~11).
- **Проблема:** **проверить**: по комментариям в `ci.yml` (~106–108) и
  `scripts/seed-demo.sh` (~20–21), `--wait` падает на завершившихся контейнерах
  (migrate, minio-init, kibana-init).
- **Исправление:** повторить в Makefile логику CI (`--wait` по списку
  долгоживущих сервисов, затем одноразовые) или вынести одноразовые в профиль.

### [ ] E5. Минимальные привилегии — Medium
- **Файлы:** `compose.yaml` (~165, 253, 266, 270–271, 298, 329), новый init-скрипт
  PostgreSQL, `minio-init`.
- **Проблема:** все сервисы (включая публичный address-api и exporter) ходят
  в PostgreSQL под суперпользователем; импортёр ходит в MinIO под root.
- **Исправление:**
  - роли PostgreSQL: `gar_migrator` (DDL), `gar_writer` (importer), `gar_reader`
    (indexer, address-api), `pg_monitor` (exporter);
  - отдельный пользователь MinIO с политикой на бакет.

### [ ] E6. Filebeat: root и `docker.sock` — Medium
- **Файлы:** `compose.yaml` (~75, 80), `deploy/filebeat.yml`.
- **Исправление:** убрать `docker.sock` и брать метаданные из путей
  `/var/lib/docker/containers`, либо поставить docker-socket-proxy только
  на чтение containers.

### [ ] E7. Ресурсы и настройки контейнеров — Medium
- **Файлы:** `compose.yaml`.
- **Что сделать:**
  - лимиты памяти и CPU (ES около 2g, postgres, importer, kibana);
  - Redis: `--maxmemory 256mb --maxmemory-policy allkeys-lru`, без `appendonly`
    (это кэш);
  - `bootstrap.memory_lock: "true"` или убрать `ulimits.memlock` (~29–32).

### [ ] E8. Аутентификация и prod-профиль — Medium
- **Файлы:** `compose.yaml` (~27, 88, 149), README.
- **Исправление:**
  - `compose.prod.yaml` с xpack security, паролями Redis/NATS, без проброса
    служебных портов;
  - в README явная пометка «dev only» для базового compose.

### [ ] E9. Образы и Dockerfile — Low
- **Файлы:** `compose.yaml` (~110, 129, 147, 237, 247–263), `services/*/Dockerfile`,
  `.dockerignore`.
- **Что сделать:**
  - MinIO с `bitnamilegacy/*` (заморожен) на `minio/minio` + `minio/mc`
    с закреплённым RELEASE;
  - закрепить `redis:7-alpine` до патч-версии (и в
    `services/address-api/internal/app/app_integration_test.go` ~550);
  - Jaeger v1 → `jaegertracing/jaeger` v2;
  - healthcheck у jaeger, filebeat, renderer и exporter'ов; зависимость grafana
    от renderer;
  - общий `image:` для migrate и importer;
  - единые флаги `-trimpath -ldflags "-s -w"` во всех Dockerfile, убрать лишний
    `apk add wget`;
  - в `.dockerignore` добавить `tests/`, `.github/`, `scripts/`, `deploy/`, `build/`.

### [ ] E10. CI — Medium
- **Файлы:** `.github/workflows/ci.yml`, `.golangci.yml` (~35, 51).
- **Что добавить:**
  - `go test -race`;
  - проверку сгенерированного кода: `go generate ./... && git diff --exit-code`
    (ent, proto, `*_gen.go`, копия OpenAPI);
  - `gar-migrate-diff` против testcontainers-postgres (схема Ent = миграции);
  - `buf lint` и `buf breaking`;
  - `go mod tidy -diff`, shellcheck, hadolint;
  - закрепить экшены по SHA;
  - в `.golangci.yml` исключать только сгенерированный `ent/`, но не `ent/schema`.

### [ ] E11. Скрипты, Makefile, README — Low
- **Файлы:**
  - `scripts/seed-demo.sh`:
    - `--limit` из справки не работает (~7, 14);
    - `$KEY` подставляется в `bash -c` — shell-инъекция через имя файла (~31);
    - `cd` выполняется до разрешения относительного пути (~10).
  - `scripts/make-demo-zip.sh`:
    - абсолютный путь к результату ломается (~49);
    - `zip -r` дописывает в существующий архив;
    - `grep` под `pipefail` молча завершает скрипт (~34).
  - `scripts/chaos/kill-elasticsearch.sh`:
    - не генерирует фикстуру (~42);
    - комментарий «30 с» при `OUTAGE_SECS=45`.
  - Chaos-скрипты не запускают фоновую нагрузку k6, о которой говорит
    `docs/report/fault-tolerance.md`.
  - `tests/load/run.sh`:
    - `VUS`/`DURATION` не передаются в контейнер;
    - мёртвая строка (~11).
  - `Makefile`:
    - нет `smoke-seed` в `.PHONY`;
    - справка `load` неточна;
    - `seed-demo` каждый раз пересобирает архив — нужна файловая цель.
  - `proto/generate.go` (~14): жёсткий PATH, работающий только с Homebrew.
  - README:
    - пути к docs (~260–262) → `../../docs/02 Форматы`,
      `../../docs/01 Руководства/…`, `../../docs/03 XSD`;
    - «up/down миграции» (~186) — раннер применяет только up.
  - `deploy/prometheus.yml` (~30): устаревший комментарий.

---

## Журнал выполнения

| Дата | Задача | Статус | Коммит / примечание |
|---|---|---|---|
| 2026-10-20 | E1 | [x] | Исправлен healthcheck Elasticsearch; изолированный контейнер без ES переходит в unhealthy, compose config валиден. |
| 2026-10-20 | E2 | [x] | Smoke проверяет job и health одного target через jq; absent/down отвергаются, UP принимается, bash -n пройден. |
