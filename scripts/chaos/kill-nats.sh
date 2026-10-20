#!/usr/bin/env bash
# kill-nats.sh — хаос-эксперимент: отказ NATS (JetStream) под нагрузкой.
#
# Фаза A: остановка NATS на 30 с во время фоновой нагрузки k6.
#   Ожидание: REST API не зависит от NATS (suggest → ES, details → PG/Redis),
#   поэтому запросы продолжают обслуживаться; importer/indexer теряют
#   соединение и уходят в not-ready, после старта NATS — переподключение.
# Фаза B: durability JetStream: при остановленном indexer повторный seed
#   фикстуры публикует gar.objects.changed, сообщения ждут в потоке GAR,
#   после подъёма indexer доезжают и индекс пересобирается.
#
# Использование: scripts/chaos/kill-nats.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
# shellcheck source=lib.sh
source scripts/chaos/lib.sh

OUTAGE_SECS=30
ZIP="build/tiny-gar.zip"

# Страховка: при любом выходе вернуть стек в рабочее состояние.
restore() {
  log "RESTORE: возвращаю nats и indexer"
  docker compose start nats >/dev/null 2>&1 || true
  docker compose start indexer >/dev/null 2>&1 || true
}
trap restore EXIT

log "===== ХАОС: NATS ====="

log "--- Фаза A: отказ NATS на ${OUTAGE_SECS} с под нагрузкой"
log "Базовые метрики до отказа:"
log "  pending importer: $(jsz_pending importer)"
log "  pending indexer:  $(jsz_pending indexer)"
log "  API RPS (1m):     $(prom_scalar 'sum(rate(platform_http_requests_total{job="address-api",path=~"/api/v1/.*"}[1m]))')"
probe_api "до"

log "STOP nats"
docker compose stop nats
STOP_TS=$(date +%s)

# Опрос во время простоя: API жив, importer/indexer — нет.
deadline=$((STOP_TS + OUTAGE_SECS))
while [ "$(date +%s)" -lt "$deadline" ]; do
  probe_api "nats down"
  log "  importer/readyz=$(http_code http://127.0.0.1:9211/readyz) indexer/readyz=$(http_code http://127.0.0.1:9212/readyz)"
  sleep 6
done

log "START nats (ожидание healthy до 90 с)"
docker compose start nats >/dev/null
NATS_HEALTHY_IN=$(wait_healthy nats 90) && log "nats healthy через ${NATS_HEALTHY_IN} с после start" || log "!!! nats не поднялся за 90 с"

# Восстановление консьюмеров: ждём readyz importer и indexer.
t0=$(date +%s)
IMP_OK=""
IDX_OK=""
while [ $(( $(date +%s) - t0 )) -lt 120 ]; do
  [ -z "$IMP_OK" ] && [ "$(http_code http://127.0.0.1:9211/readyz)" = "200" ] && IMP_OK=$(( $(date +%s) - t0 )) && log "importer /readyz 200 через ${IMP_OK} с после healthy NATS"
  [ -z "$IDX_OK" ] && [ "$(http_code http://127.0.0.1:9212/readyz)" = "200" ] && IDX_OK=$(( $(date +%s) - t0 )) && log "indexer /readyz 200 через ${IDX_OK} с после healthy NATS"
  [ -n "$IMP_OK" ] && [ -n "$IDX_OK" ] && break
  sleep 1
done
probe_api "после восстановления"
log "  pending importer: $(jsz_pending importer)"
log "  pending indexer:  $(jsz_pending indexer)"
log "  jetstream всего сообщений в потоке: $(prom_scalar 'jetstream_stream_total_messages{stream_name="GAR"}')"

log "--- Фаза B: durability JetStream (indexer остановлен, события копятся)"
[ -f "$ZIP" ] || { log "генерирую фикстуру $ZIP"; go run ./cmd/gar-mkfixture --out "$ZIP"; }

PENDING_BEFORE=$(jsz_pending indexer | cut -d' ' -f1)
DOCS_BEFORE=$(indexer_docs_indexed)
log "  до: pending indexer=$PENDING_BEFORE, documents_indexed=$DOCS_BEFORE"

log "STOP indexer"
docker compose stop indexer

log "Повторный seed фикстуры ($ZIP) — importer обработает и опубликует gar.objects.changed"
scripts/seed-demo.sh "$ZIP" >/dev/null

# Ждём, пока importer отработает: его очередь пустеет, у indexer копится.
t0=$(date +%s)
while [ $(( $(date +%s) - t0 )) -lt 180 ]; do
  IMP_PEND=$(jsz_pending importer | cut -d' ' -f1)
  IDX_PEND=$(jsz_pending indexer | cut -d' ' -f1)
  if [ "$IMP_PEND" = "0" ] && [ "$IDX_PEND" -gt "$PENDING_BEFORE" ] 2>/dev/null; then
    break
  fi
  sleep 3
done
log "  при остановленном indexer: pending importer=$IMP_PEND, pending indexer=$IDX_PEND (накопились $((IDX_PEND - PENDING_BEFORE)) событий)"

log "START indexer"
docker compose start indexer >/dev/null
IDX_HEALTHY_IN=$(wait_healthy indexer 120) && log "indexer healthy через ${IDX_HEALTHY_IN} с" || log "!!! indexer не поднялся за 120 с"

# Ждём, пока indexer доест накопленное.
t0=$(date +%s)
while [ $(( $(date +%s) - t0 )) -lt 120 ]; do
  IDX_PEND=$(jsz_pending indexer | cut -d' ' -f1)
  [ "$IDX_PEND" = "0" ] && break
  sleep 3
done
# Счётчик documents_indexed обнуляется при рестарте процесса и появляется
# в Prometheus после первого scrape — ждём свежего значения (до 60 с).
t0=$(date +%s)
DOCS_AFTER="n/a"
while [ $(( $(date +%s) - t0 )) -lt 60 ]; do
  DOCS_AFTER=$(indexer_docs_indexed)
  [ "$DOCS_AFTER" != "n/a" ] && break
  sleep 5
done
log "  после подъёма indexer: pending indexer=$IDX_PEND, documents_indexed: $DOCS_BEFORE → $DOCS_AFTER (счётчик нового процесса)"

log "===== ХАОС NATS ЗАВЕРШЁН ====="
