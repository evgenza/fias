# Legacy SQL-планы миграций

Эти файлы — исторические SQL-планы, которые раньше генерировала команда
`migrate --write-sql` из схем Ent. Они **не применялись** как версионированная
история и не применяются раннером `internal/migrations`; хранятся как .txt
во избежание случайного применения.

Рабочая история миграций: `migrations/atlas/` (формат Atlas: `*.up.sql`,
`*.down.sql`, `atlas.sum`). Новые изменения схемы: `go run ./cmd/gar-migrate-diff
-name <имя> -dev-url <одноразовая база>` на пустой dev-базе, применяющей
текущий каталог.

## Переход legacy-базы с заполненной таблицей param

Историческая проблема: `0002_param_source.sql` добавлял `source_id bigint
NOT NULL` без backfill — на заполненной базе миграция невыполнима. Происхождение
(`source`) старых строк параметров не может быть восстановлено из самих данных:
не выдумывайте семейство и не очищайте таблицу.

Безопасный путь (выполняется вручную psql после бэкапа):

1. Добавить nullable-столбцы:

   ```sql
   ALTER TABLE gar.param ADD COLUMN IF NOT EXISTS source text;
   ALTER TABLE gar.param ADD COLUMN IF NOT EXISTS source_id bigint;
   ```

2. Backfill с фиксацией неизвестного происхождения: `source` остаётся NULL
   для старых строк (не заполнять значением семейства без доказательств!).
   Если происхождение известно из внешних метаданных — заполнить им.
3. Проверить коллизии бизнес-ключа: `SELECT source_id, count(*) FROM gar.param
   GROUP BY 1 HAVING count(*) > 1;` (для NULL-источника ключ не уникален —
   уникальный индекс вводится только на строки с source).
4. Только затем NOT NULL/уникальность — частичный индекс:

   ```sql
   CREATE UNIQUE INDEX IF NOT EXISTS param_source_id_idx
     ON gar.param (source, source_id) WHERE source IS NOT NULL;
   ```

5. Выполнить `gar-importer migrate --baseline`: раннер проверит наличие всех
   таблиц и столбцов ожидаемой схемы (включая `param.source`, `param.source_id`)
   и только после этого запишет историю применённых версий в `gar_control`.
   Если legacy-базу привести к ожидаемой схеме невозможно, baseline даст
   ошибку с перечнем отличий; данные сохраняются, восстановление предлагается
   в новом dataset из исходного архива (этапы 6–7).
