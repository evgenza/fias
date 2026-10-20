#!/usr/bin/env bash
# kill-postgres.sh — хаос-эксперимент: отказ PostgreSQL под нагрузкой.
#
# Остановка PostgreSQL на 30 с во время фоновой нагрузки k6.
#   Ожидание: /api/v1/readyz → 503 (пинг БД обязателен), карточки без кэша
#   → 500 INTERNAL (ошибка БД), НО закэшированные карточки продолжают
#   отвечать 200 из Redis (X-Cache: hit) — кэш переживает отказ БД.
#   /api/v1/suggest продолжает работать: путь подсказок идёт через
#   Elasticsearch и не зависит от PostgreSQL (изоляция отказов).
#
# Использование: scripts/chaos/kill-postgres.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
# shellcheck source=lib.sh
source scripts/chaos/lib.sh

OUTAGE_SECS=30
WARM_IDS="101 102 103 104 105 106 201 202 203 204 205 301 302 303 304 306 401 501"

restore() {
  log "RESTORE: возвращаю postgres"
  docker compose start postgres >/dev/null 2>&1 || true
}
trap restore EXIT

log "===== ХАОС: POSTGRESQL ====="

# Прогрев кэша Redis: все карточки корпуса → X-Cache: miss, запись в кэш.
log "Прогрев кэша карточек (${WARM_IDS})"
for id in $WARM_IDS; do
  curl -s -o /dev/null --max-time 5 "$API/api/v1/addresses/$id" || true
done

PG_CONN_BEFORE=$(prom_scalar 'sum(pg_stat_activity_count{datname="gar"})')
log "Базовые метрики до отказа: pg connections=$PG_CONN_BEFORE"
probe_api "до"

log "STOP postgres"
docker compose stop postgres
STOP_TS=$(date +%s)

deadline=$((STOP_TS + OUTAGE_SECS))
while [ "$(date +%s)" -lt "$deadline" ]; do
  probe_api "pg down"
  log "  details/101 (кэш): $(curl -s -o /dev/null -w '%{http_code} X-Cache:%header{x-cache}' --max-time 5 "$API/api/v1/addresses/101")" \
      " details/999999 (мимо кэша): $(http_code "$API/api/v1/addresses/999999")"
  log "  importer/readyz=$(http_code http://127.0.0.1:9211/readyz) indexer/readyz=$(http_code http://127.0.0.1:9212/readyz)"
  sleep 6
done

log "START postgres (ожидание healthy до 90 с)"
docker compose start postgres >/dev/null
PG_HEALTHY_IN=$(wait_healthy postgres 90) && log "postgres healthy через ${PG_HEALTHY_IN} с после start" || log "!!! postgres не поднялся за 90 с"

# Восстановление API: пул соединений переподключается сам.
API_RECOVERED_IN=$(wait_http_200 "$API/readyz" 120) \
  && log "address-api /readyz 200 через ${API_RECOVERED_IN} с после healthy postgres" \
  || log "!!! /readyz не восстановился за 120 с"

# Проверка, что карточки из БД снова читаются (мимо кэша → новый miss).
log "  details/306 после восстановления: $(curl -s -o /dev/null -w '%{http_code} X-Cache:%header{x-cache}' --max-time 5 "$API/api/v1/addresses/306")"
log "  pg connections: $PG_CONN_BEFORE → $(prom_scalar 'sum(pg_stat_activity_count{datname="gar"})')"
probe_api "после восстановления"

log "===== ХАОС POSTGRESQL ЗАВЕРШЁН ====="
