# Журнал рефакторинга

Последовательные этапы из задания; по каждому — изменения, контракт, выполненные
проверки и оставшиеся ограничения.

## Этап 0. Зафиксировано базовое поведение (2026-10-07)

Подробности в [baseline.md](baseline.md), каталог форматов в
[format-catalog.md](format-catalog.md).

## Этап 1. Приём некорректных записей закрыт

**Изменено**

- Закреплены исходные XSD ФИАС (каталог `xsd/`, 22 файла, сверены по sha256).
- `cmd/gar-gen-spec` генерирует `internal/garxml/spec_gen.go` — спецификации
  атрибутов 23 форматов из XSD (обязательность, xs:date/integer/long/boolean,
  minLength/maxLength/length, PATH как anySimpleType). Перегенерация
  `go generate .`, проверка актуальности `-verify`.
- `internal/garxml/spec.go` — движок проверки: присутствие обязательных
  атрибутов, различие «атрибут отсутствует» / «пустое значение», длина строк,
  GUID 36, диапазон int64 для xs:long, Calendar-дата, `false` и `"0"` — валидны.
  Неизвестный атрибут — ошибка (строгий режим: новые данные не теряются молча).
- `internal/importer/zip.go` — валидация выполняется до декодирования в builder,
  то есть до дедупликации и SQL.

**Контракт**: запись не доходит до дедупликации/writer, если не удовлетворяет
закреплённому XSD формата; ошибка структурирована
(формат, атрибут, причина + файл, номер записи и смещение XML от DecodeXML).

**Проверено**: `go test ./...` (23 fixture-записи валидны; удаление каждого
обязательного атрибута каждой записи даёт ошибку с именем атрибута; значения
по правилам); `go vet ./...`; `-verify` генерации; integration `TestZIPToAllTables`
на временной БД PostgreSQL 18 (создана и удалена) — пройти с валидацией.

**Ограничения**: дата и последовательность имени файла проверяются только
структурно (8 цифр) — календарность закрывает этап 2; ошибки SQL-пакета всё ещё
не указывают на конкретную строку (этап 4); вложенные элементы внутри записи не
проверяются (в XSD записи атрибутные); строгий режим не имеет переключателя
политики для новых форматов — по мере надобности на этапе 2/4.

## Этап 2. Единый каталог форматов и план архива

**Изменено**

- Новый пакет `internal/gar` — единый каталог: `Kind`, элементы (root/record),
  разбор `ParseFile` (теперь с календарной датой имени файла — дефект
  `20269999` закрыт), `RegionCode` с явной поддержкой обёрток `gar_xml/` и
  `gar_delta_xml/` (неизвестная папка — ошибка, не «общероссийский файл» —
  дефект закрыт), нормализация источников параметров, сгенерированные
  спецификации атрибутов (переехали из `internal/garxml`).
- План архива `gar.Plan` + фильтры `gar.Select`: статус каждого XML-файла
  (selected/filtered/unknown/duplicate) до начала импорта; неизвестный
  формат отделён от повреждённого имени известного; голый `AS_PARAM`
  без семейства объектов — unknown, а не неоднозначный источник в БД;
  повторный путь — duplicate (побеждает первое вхождение).
- `--formats AS_HOUSES_PARAMS` теперь точный фильтр семейства (не расширяется
  до всех шести); `--formats AS_PARAM` по-прежнему выбирает все шесть.
- `internal/garxml` оставлен только потоковый декодер и импортирует `gar`;
  `ent/template` и `importer` используют `gar.Kind`.

**Контракт**: одно определение региона и формата во всех слоях; файл с
неожиданным путём или неоднозначным источником не попадает в БД; все
неподдержанные файлы отражены в отчёте с причиной; дата имени файла —
календарный день.

**Проверено**: `go test ./...` (план/фильтры/регионы/имена/классификация
ошибок); `go vet ./...`; воспроизводимость `go generate .` и `-verify`;
integration `TestZIPToAllTables` на временной БД PostgreSQL (создана и удалена).

**Ограничения**: покрытие выпуска (manifest) ещё не связывается с журналом
выпусков — это этапы 6–7; `sequence` файла трактуется как идентификатор без
интерпретации порядка; дубликаты ключей между файлами resolve на upsert
(последняя запись побеждает) — единая политика фиксируется в этапах 4/6.

## Этап 3. Независимые записи и декодирование

**Изменено**

- `cmd/gar-gen-records` генерирует `internal/gar/record`: типизированные
  записи 23 форматов (поля и XML/JSON-теги повторяют Ent-модели; зависимость
  от Ent — только на этапе генерации) и `record.DecodeStream` — потоковый
  декодер, возвращающий записи в типизированный `record.Handler`.
  Валидация атрибутов выполняется декодером до передачи записи обработчику.
- `ent/template/gar_batch.tmpl` переписан: адаптер реализует `record.Handler`,
  строит Ent-builders из записей, сам пакетирует (`batchSize`) и считает
  подтверждённо записанное (`Committed()`), уведомляет колбэком `SetOnCommit`.
  Дедупликация и upsert не изменились. Строки `gar_…Batch` декодируют XML
  больше не сами.
- `internal/importer` декодирует через `record.DecodeStream`; XML-пакеты
  (`encoding/xml`) из сценария убраны.
- `internal/model` (алиасы Ent) удалён: тесты перенесены — XML/JSON round-trip
  и fixtures в `internal/gar/record`, типовые тесты Date/Flag в `internal/gartype`.
- Гарантия отмены: `DecodeStream` проверяет `ctx` между токенами; отмена
  заблокированного чтения источника — ответственность источника (документировано).

**Контракт**: XML декодируется и проверяется без Ent и БД; запись с ошибкой
не доходит до builder; генерация воспроизводима (`-verify`); JSON-представление
исходных записей сохранено тестами round-trip.

**Проверено**: `go test ./...`; `go vet ./...` (+integration-тег);
воспроизводимость `go generate .` и `-verify` обоих генераторов; integration
на временной БД PostgreSQL: все пакеты, включая `TestZIPToAllTables`
(23 формата, upsert, NULL/false, лимит, отмена, битый XML) и тесты storage.

**Ограничения**: orchestrация и счётчики пока в `internal/importer`
(разделение writer/сценария — этап 4); `Committed()` считает dedup-операции
записи, отдельные счётчики decoded/committed/deduplicated введёт этап 4.

## Этап 4. Сценарий, пакетный writer и тестируемый CLI

**Изменено**

- `internal/importjob` — сценарий импорта: план архива, фильтры, поток записей,
  пакетирование, прогресс, отчёт, остановка. Порты `Archive` (источник XML)
  и `Writer` (пакетная запись) объявлены со стороны сценария; Ent, ZIP
  и pgx сценарию не видны (`importjob` не импортирует ent/sql/zip/xml).
- `internal/input/zipgar` — источник: инвентаризация ZIP и открытие XML
  без распаковки на диск (реализация порта Archive).
- `internal/storage` — адаптер Ent: `Store.Writer(kind, source, batchSize)`
  реализует порт WriterFactory; генерируемые адаптеры (`ent/template`)
  содержат builders, дедупликацию, upsert и счётчики; путь записи — прежний
  `CreateBulk` + `OnConflictColumns`. Отдельные счётчики: подтверждённые
  входные записи (`Committed`) и дедуплицированные операции записи
  (`WriteOps`); оба растут только после успешного коммита пакета.
- `internal/cli` — тестируемая `Run(ctx, args, stdout, stderr) error`;
  `os.Exit` остался только в `main`. Флаги и семантика команд прежние.
- `internal/config` — env/.env, настройки пула (defaults прежние), таймаут
  подключения, маскирование пароля DSN в ошибках. Опции сценария
  (`ValidateOptions`) проверяются до открытия БД.
- `internal/importer` удалён (заменён importjob); integration-тест переведён
  на реальные коммиты со сбросом схемы (без откатываемой транзакции).

**Контракт**: counters не завышаются при неизвестном исходе коммита; unit-тесты
с fake writer покрывают выбор файлов, лимит, флаш по размеру пакета, ошибки
декодирования/записи/коммита и отмену без БД; Ent скрыт за адаптером.

**Проверено**: `go test ./...` (включая новые unit-тесты importjob и CLI);
`go vet ./...`; e2e smoke: `migrate` (23 таблицы) + `import` реального ZIP
со счётчиками, фильтрами регионов и отчётом о повреждённом имени файла;
integration `-p 1` на временной БД: все пакеты зелёные.

**Ограничения**: `--write-sql` по-прежнему не dry-run, Schema.Create — не
версионированная история (этап 5); checkpoint и журнал выпусков отсутствуют
(этапы 6–7); pool-настройки применяются, но не настраиваются через CLI-флаги
(окружение `GAR_MAX_OPEN_CONNS`/`GAR_MAX_IDLE_CONNS`); интеграционные пакеты
требуют последовательного запуска или отдельных баз (задокументировано в README).

## Этап 5. Версионированные миграции

**Изменено**

- `internal/migrations` — раннер версионированных миграций в формате Atlas
  (`migrations/atlas/`: `*.up.sql`/`*.down.sql` + `atlas.sum`): проверка
  каталога (migrate.Validate), применение по одной версии в транзакции вместе
  с записью ревизии (`gar_control.atlas_schema_revisions`, Atlas-совместимый
  формат), advisory-lock на закреплённом соединении, sha256 уже применённых
  версий с отказом при несовпадении.
- `cmd/gar-migrate-diff` — генерация следующей миграции сравнением фактического
  состояния одноразовой dev-базы с ent/schema (`schema.NamedDiff`); целевая
  база не участвует. Начальная миграция `20261006224052_gar_init` сгенерирована
  этим путём.
- CLI `migrate`: применяет версионированные миграции; `--plan` — только показать
  ожидающие (read-only: не создаёт даже таблицу ревизий); `--baseline` — пометка
  legacy-базы после проверки фактических таблиц/столбцов против метаданных Ent.
  `Schema.Create` и `--write-sql` удалены — один механизм применения; дефект
  «--write-sql не dry-run» устранён.
- `migrations/legacy/` — исторические SQL-планы (как .txt, не применяются) и
  процедура восстановления заполненной legacy-`param` (nullable → проверяемый
  backfill → NOT NULL → частичный уникальный индекс, без выдумывания source).
- Ent-CLI Atlas (v0.13.1) не закреплён как tool: несовместим с текущим
  tablewriter; master не собирается на macOS из-за pg_query_go. Формат каталога
  и реестра — Atlas-совместимый (см. docs/decisions.md).

**Контракт**: переходы «пустая БД → текущая», повторный apply (no-op), отказ при
подделке checksum, сериализация параллельного apply, plan не изменяет целевую
БД, baseline не помечает схему вслепую.

**Проверено**: integration-тесты на отдельной PostgreSQL: пустая база (23
таблицы + ревизии), повторный apply, tampered hash, два параллельных раннера,
baseline-отказ на неполной legacy-схеме, baseline-успех после приведения схемы;
`go test ./...`; e2e: `migrate` → `migrate` (актуальны) → `--plan` → `import`.

**Ограничения**: down-миграции раннером не исполняются (только forward);
генерация diff требует одноразовую dev-базу (docker compose) — автоматическая
`docker://` dev-база недоступна без Atlas CLI; миграция выполняется целиком на
доступности командной строки (нет CI-задачи). Прочее — этапы 6–8.

## Этап 6. Журнал, checkpoint и возобновление

**Изменено**

- `internal/control` — журнал в схеме `gar_control`: `datasets` (legacy `gar`
  регистрируется без переименований), `import_runs` (mode, source digest,
  options, состояние), `import_files` (entry identity, счётчики,
  last_committed_record, integrity_checked), `publications` (указатель
  активного dataset — заполняется этапом 7). Состояние выполнения отделено
  от состояния публикации; sample не разрешает публикацию (`sampled`).
- Идентичность источника — потоковый sha256 содержимого архива
  (`zipgar.Digest`); имя/дата/размер и кеш по mtime не используются.
- Атомарный CommitBatch: сгенерированный адаптер выполняет каждый пакет в
  транзакции Ent, checkpoint файла обновляется в той же транзакции через
  драйвер tx (`dialectExec`); сбой checkpoint откатывает пакет, счётчики
  не завышаются.
- Возобновление: `--resume` продолжает последний незавершённый запуск с тем же
  digest; завершённые файлы (complete + integrity_checked) пропускаются,
  незавершённый файл открывается заново, подтверждённые записи валидируются
  без записи (SetSkip; O(1) seek по XML-offset не заявляется), новые — пишутся.
  Подмена архива (digest) или режима (sample↔snapshot) отвергается.
- Сериализация записи: advisory-блокировка dataset на выделенном соединении;
  второй writer получает ошибку, а не молча пишет; потеря соединения прекращает
  владение. Статус canceled/failed фиксируется ограниченным по времени
  контекстом после отмены основного.

**Контракт**: данные и checkpoint атомарны; counters не завышаются при
неизвестном исходе коммита; resume без пропусков; второй writer не меняет
dataset параллельно; sample не публикуется.

**Проверено**: unit-тесты портов с fake writer; integration
`TestJournalResumeAndLock`: реальный коммит → kill процесса (e2e `kill -9`
на 60 000 записях: checkpoint=1000, run=loading) → новый процесс `--resume`
дочитал 59 000 из 60 000 → в БД 60 000 строк / 60 000 различных ID (без
пропусков и дублей); подмена digest и смена режима отвергнуты; complete-файл
пропускается при повторном resume; блокировка второго writer проверена.
Весь integration-набор зелёный (`-p 1`).

**Ограничения**: приложения applied_releases появятся с дельтами (этап 8);
публикация и отдельные схемы snapshots — этап 7; digest вычисляется полным
чтением архива каждый запуск (стоимость осознанна, потоково); resume требует
тот же режим и digest, но не сверяет покомпонентно options-фильтры регионов
(фиксируется как открытый пункт этапа 7 вместе с манифестом выпуска).

## Волна 0 / E1 (2026-10-20)

Исправлен healthcheck Elasticsearch; изолированный контейнер без ES переходит в unhealthy, compose config валиден.

## Волна 0 / E2 (2026-10-20)

Smoke проверяет job и health одного target через jq; absent/down отвергаются, UP принимается, bash -n пройден.

## Волна 0 / E3 (2026-10-20)

Удалён .idea из индекса, добавлен ignore, пароли заменены change_me, DATABASE_URL использует POSTGRES_PORT; compose config валиден.

## Волна 0 / A3 (2026-10-20)

Ошибки данных больше не повторяются как временные; диапазоны int64 по Ent-моделям, XSD enum, UUID, RecordError при декодировании, sentinel пустого выбора, ZIP/flate/SQLSTATE 22 и 23. Регрессии воспроизведены до исправления; build, vet, lint, unit, integration и оба генератора -verify прошли.

## Волна 0 / D3 (2026-10-20)

Ограничена кардинальность метрик и спанов; методы вне списка дают OTHER, имена спанов берутся из r.Pattern, удалённый sampled-флаг не управляет выборкой, baggage не переносится. Unit-тесты, build, vet, lint и integration прошли.

## Волна 0 / D1 (2026-10-20)

Readiness зависит от PostgreSQL, будит IDLE gRPC и публикует состояние Redis/suggester в метриках; клиент получает только not ready, подробности логируются. Тест на отказ необязательных зависимостей и пробуждение IDLE, build, vet, lint, unit и integration прошли.

## Волна 1 / A1 (2026-10-20)

Подписчик продлевает lease сообщения каждые 15 с до Ack/Nak/Term или отмены контекста; следующий Fetch(1) выполняется после завершения текущего сообщения. Consumer обновляется с явным AckWait=30 с без BackOff; повторы через NakWithDelay. Реальный NATS подтвердил старое ожидание 1 с и отсутствие повторов при обработке 35 с. Общие build/vet/lint/unit/integration прошли.

## Волна 1 / A2 (2026-10-20)

После десяти неудачных обработок сообщение сохраняется в gar.dlq.<subject> с исходными байтами, причиной и числом доставок; только затем Term. Постоянные ошибки и битые JSON тоже сохраняются. Публикации дедуплицируются по stream/consumer/sequence; отказ DLQ оставляет исходное сообщение для повтора. Поэтому серверный MaxDeliver=-1, прикладной лимит=10: конечный серверный лимит оставлял бы сообщение недоступным после отказа DLQ. Worker и indexer передают причину Retry; сценарий импорта фиксирует failed и исходную ошибку в журнале. Метрика gar_events_terminated_total и ERROR-лог доступны в обоих сервисах.

GAR ограничен 30 сутками и 10 GiB; DiscardNew отклоняет новые публикации вместо вытеснения старых по размеру. DeliverAll для нового durable индексатора сохранён намеренно: пересоздание durable используется для восстановления индекса из доступной истории; DeliverNew пропускал бы ожидающие события. Для восстановления за пределами retention требуется полный импорт. Регрессия на реальном NATS и общий набор проверок пройдены.

## Волна 1 / B1 (2026-10-20)

Checkpoint получает committed+received (адаптер добавляет base при resume). Ent-шаблон увеличивает счётчики, очищает builders и вызывает onCommit только после успешного tx.Commit; сгенерирован go generate ./ent. При отказе COMMIT пакет остаётся доступен повторному Save, счётчики и callback не продвигаются. decoded переведён в bigint, существующая служебная таблица обновляется при Open. Тест с deferred constraint воспроизвёл ложный прогресс до исправления; после четырёх пакетов checkpoint совпадает с числом строк, resume пишет ровно остаток. Все проверки прошли с отдельной PostgreSQL.

## Wave 1 / B2 (2026-10-20)

Worker uses ResumeRunID=-1 with ResumeOrNew=true. Resume validates digest, mode, Regions/Kinds/Sources and LimitPerFile as normalized sets; batch size may change. All unfinished states are eligible. Selection and status reset to loading run under the dataset lock, released after FinishRun. Incompatible resume is permanent. Removed unused Options.SchemaName: storage/Ent use gar. Cancellation/resume of the same run, completed-file skipping, filter validation and all required checks passed.

## Wave 1 / C1 (2026-10-20)

Builder serializes dictionary loading with a mutex and returns immutable snapshots. Empty startup results are not cached; other snapshots expire after one minute. The indexer invalidates the cache on region-0 house/add-house/apartment/room type events; API instances refresh via TTL. Real PostgreSQL tests reproduced the stale empty cache and a data race before the fix. Empty-to-import, TTL, invalidation and 24 concurrent DescribeObject callers now pass with -race. All required checks passed.

## Wave 1 / C4 (2026-10-20)

Address analyzers normalize yo to e without stopwords; prefix ngrams cover 1..20 characters and query tokens truncate to 20. Full text retains complete tokens for fuzzy search. ES_INDEX now names a write alias to ES_INDEX-v2 with mapping version metadata; bulk requires an alias and cannot auto-create dynamic mappings. Fresh installations create mapping and alias atomically. Legacy physical indices require the explicit offline command cmd/gar-search-migrate: stop writers, block legacy writes, refresh, reindex, reject failures/timeouts/count mismatches, then atomically replace the legacy physical index with the alias. Failed copies preserve the source and can be retried. README and docs/search-index-migration.md explain recovery and snapshot/rollback requirements. No live platform migration was run. Real ES tests cover short/long tokens, yo/e, stopword preservation, migration and retry after mapping failure, idempotence and alias guards. Build/vet/lint/unit/integration passed.

## B3 — повторная задача импорта

Готовый запуск с тем же digest и отбором повторно используется под блокировкой dataset. События используют digest + путь как Msg-Id (старые события без digest совместимы). Для установления digest содержимое S3 скачивается; ETag не считается SHA-256.

## B4 — порядок выпусков

Импорт принимает mode/base_release/target_release (CLI: --mode, --base-release, --target-release; такие же поля в NATS). Snapshot определяет выпуск из version.txt/имён XML, delta требует явной базовой версии и полного отбора. Только полный неограниченный импорт продвигает applied_release. Старые архивы и разрывы цепочки отклоняются до записи. Неудавшийся архив резервирует dataset до успешного resume; другой архив ждёт восстановления. Worker держит текущую задачу при временных ошибках, MaxAckPending=1 сохраняет порядок после рестарта. Upsert не понижает update_date.

## C2 — постраничная сборка адресов

Региональная иерархия определяет кандидатов, LATERAL берёт последнюю запись по составному индексу. Каждая страница содержит до 2000 объектов; SQL rows закрывается до загрузки предков и передачи в Elasticsearch. Добавлена миграция индексов (схемы Ent согласованы).

## C3 — каскад событий индексатора

Изменения предков, реестра, административной иерархии и параметров пересобирают зависимые документы региона. Муниципальная иерархия не используется адресным сборщиком и не вызывает пересборку. Выбран явный граф зависимостей вместо debounce по журналу: каждое событие можно независимо повторить.

## C6 - current documents and stale index cleanup

Hierarchy is resolved across regions before checking the requested region. Inactive/non-actual objects are excluded. A successful regional kind rebuild refreshes Elasticsearch and deletes older documents of that region and kind. Failed builds skip cleanup; delete_by_query timeouts, conflicts and failures fail the event.

Wave 2 verification: build/vet/lint/unit and full integration passed. Preview events include sample_limit in Msg-Id to preserve the subsequent full-import event. Ready archive skips are logged. source_release protects data loaded by partial imports; only full imports advance applied_release.

## C7. Правила формирования адресной строки (2026-10-20)

- Тип дома выводится только вместе с соответствующим номером. Если HOUSENUM
  отсутствует, строка начинается с первого дополнительного номера и его типа;
  поле Name содержит первый непустой номер, TypeName — соответствующий тип.
- В адресной строке объектов уровней 1, 3, 4 и их потомков используется действующий
  TYPEID=16 из AS_ADDR_OBJ_PARAMS. Проверяются начало и окончание действия;
  исторические, будущие и параметры других семейств не подменяют адрес.
  Без действующего параметра сохраняется сочетание сокращённого типа и имени.
- При отсутствии REGIONCODE регион определяется по записи корня PATH в
  административной иерархии. Если административных записей объекта нет,
  используется его актуальная муниципальная иерархия. При наличии обеих
  административная имеет приоритет; документы не дублируются. Нужен известный
  REGIONCODE самого объекта либо корня: OBJECTID в PATH не является кодом региона.
- Типы участков и машино-мест читаются из действующих ADDR_OBJ_TYPES уровней 9
  и 17. Если справочник отсутствует, выводится номер без выдуманного типа.
  Изменение справочника инвалидирует кэш; события MUN_HIERARCHY пересобирают
  все виды адресов, поскольку муниципальные пути теперь участвуют в индексации.
- Правила одинаковы для поисковых документов и DescribeObject. Golden-регрессии
  проверяют пример адреса комнаты из руководства ФНС, дом без основного номера,
  муниципальный участок, приоритет иерархий и сроки официальных наименований.
