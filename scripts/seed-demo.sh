#!/usr/bin/env bash
# seed-demo.sh — демо-засев платформы: загрузка ZIP ГАР в MinIO и постановка
# задачи импорта в NATS JetStream. Дальше цепочка автоматическая:
# importer (MinIO → PostgreSQL) → события gar.objects.changed → indexer → Elasticsearch.
#
# Использование (из корня репозитория):
#   scripts/seed-demo.sh <путь к zip> [регионы,например 77] [--limit N]
# Пример: scripts/seed-demo.sh build/gar-demo.zip 77
set -euo pipefail
cd "$(dirname "$0")/.."

ZIP="${1:?укажите путь к ZIP выгрузки ГАР}"
REGIONS="${2:-}"
LIMIT="${3:-}"
[ -f "$ZIP" ] || { echo "нет файла: $ZIP" >&2; exit 1; }
ZIP="$(cd "$(dirname "$ZIP")" && pwd)/$(basename "$ZIP")"

echo "==> инфраструктура приёмки (postgres, nats, minio)"
docker compose up -d --wait postgres nats minio
# minio-init — one-shot (создание бакета); --wait на exited-контейнерах
# в Compose v5 возвращает ошибку, поэтому запускаем синхронно через run.
docker compose run --rm -T minio-init >/dev/null

# Бакет — из итоговой конфигурации compose (учитывает .env).
BUCKET="$(docker compose config | sed -n 's/^ *MINIO_BUCKET: //p' | head -1)"
BUCKET="${BUCKET:-gar-archives}"
KEY="$(basename "$ZIP")"

echo "==> загрузка $ZIP в MinIO s3://$BUCKET/$KEY"
docker compose run --rm -T --no-deps -v "$ZIP":/tmp/import.zip:ro minio-init \
  bash -c 'mc alias set local http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null && mc cp /tmp/import.zip "local/$MINIO_BUCKET/'"$KEY"'"'

ARGS=(--s3-uri "s3://$BUCKET/$KEY")
[ -n "$REGIONS" ] && ARGS+=(--regions "$REGIONS")
[ -n "$LIMIT" ] && ARGS+=(--limit-per-file "$LIMIT")

echo "==> постановка задачи импорта в NATS: ${ARGS[*]}"
docker compose run --rm -T --no-deps importer request-import "${ARGS[@]}"

cat <<EOF

Задача принята. Наблюдение за конвейером:
  docker compose logs -f importer indexer
Проверка результата:
  curl -s localhost:9200/gar-addresses/_count | jq .count   # растёт по мере индексации
  curl -s 'localhost:8080/api/v1/suggest?q=москва' | jq .   # подсказки
EOF
