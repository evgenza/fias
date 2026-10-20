#!/usr/bin/env bash
# kill-elasticsearch.sh — хаос-эксперимент: отказ Elasticsearch под нагрузкой.
#
# Остановка ES на 30 с во время фоновой нагрузки k6.
#   Ожидание: /api/v1/suggest деградирует в 503 UNAVAILABLE (gRPC Unavailable
#   от suggester), карточки /api/v1/addresses/{id} продолжают работать
#   (PostgreSQL + Redis не зависят от ES). Suggester и indexer уходят
#   в not-ready. Во время простоя публикуется событие gar.objects.changed:
#   indexer не может записать в ES и делает Nak — после подъёма ES событие
#   доставляется повторно и индекс пересобирается (потерь нет).
#   У ES длинный start_period — ожидание healthy до 180 с.
#
# Использование: scripts/chaos/kill-elasticsearch.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
# shellcheck source=lib.sh
source scripts/chaos/lib.sh

OUTAGE_SECS=45
ZIP="build/tiny-gar.zip"

restore() {
  log "RESTORE: возвращаю elasticsearch"
  docker compose start elasticsearch >/dev/null 2>&1 || true
}
trap restore EXIT

log "===== ХАОС: ELASTICSEARCH ====="

DOCS_BEFORE=$(indexer_docs_indexed)
ERRORS_BEFORE=$(prom_scalar 'sum(indexer_events_total{result="error"})')
log "Базовые метрики до отказа:"
log "  documents_indexed=$DOCS_BEFORE, indexer events error=$ERRORS_BEFORE"
probe_api "до"

log "STOP elasticsearch"
docker compose stop elasticsearch
STOP_TS=$(date +%s)

# Публикуем событие ПОКА ES лежит: indexer примет, не сможет записать, сделает Nak.
log "Seed фикстуры во время простоя ES (событие уйдёт indexer-у, запись в ES упадёт → Nak)"
scripts/seed-demo.sh "$ZIP" >/dev/null

deadline=$((STOP_TS + OUTAGE_SECS))
while [ "$(date +%s)" -lt "$deadline" ]; do
  probe_api "es down"
  log "  suggester/readyz=$(http_code http://127.0.0.1:9213/readyz) indexer/readyz=$(http_code http://127.0.0.1:9212/readyz)"
  sleep 6
done

log "START elasticsearch (ожидание healthy до 180 с — длинный start_period)"
docker compose start elasticsearch >/dev/null
ES_HEALTHY_IN=$(wait_healthy elasticsearch 180) && log "elasticsearch healthy через ${ES_HEALTHY_IN} с после start" || log "!!! ES не поднялся за 180 с"

# Восстановление подсказок: suggester увидит ES и вернётся в ready.
SUGG_RECOVERED_IN=$(wait_http_200 "$API/api/v1/suggest?q=пер&limit=10" 120) \
  && log "suggest снова 200 через ${SUGG_RECOVERED_IN} с после healthy ES" \
  || log "!!! suggest не восстановился за 120 с"
log "  suggester/readyz=$(http_code http://127.0.0.1:9213/readyz)"

# Redelivery: indexer должен доесть событие, на которое отвечал Nak.
t0=$(date +%s)
DOCS_AFTER="$DOCS_BEFORE"
while [ $(( $(date +%s) - t0 )) -lt 120 ]; do
  DOCS_AFTER=$(indexer_docs_indexed)
  [ "$DOCS_AFTER" != "n/a" ] && [ "${DOCS_AFTER%.*}" -gt "${DOCS_BEFORE%.*}" ] && break
  sleep 3
done
ERRORS_AFTER=$(prom_scalar 'sum(indexer_events_total{result="error"})')
REDELIVERED=$(jsz_pending indexer | cut -d' ' -f2)
log "  indexer: documents_indexed $DOCS_BEFORE → $DOCS_AFTER; events error $ERRORS_BEFORE → $ERRORS_AFTER; redelivered=$REDELIVERED"
log "  документов в gar-addresses: $(curl -s http://127.0.0.1:9200/gar-addresses/_count | grep -o '"count":[0-9]*' | cut -d: -f2)"
probe_api "после восстановления"

log "===== ХАОС ELASTICSEARCH ЗАВЕРШЁН ====="
