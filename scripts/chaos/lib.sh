#!/usr/bin/env bash
# lib.sh — общие функции хаос-экспериментов платформы ГАР.
# Не исполняется напрямую; подключается через source из скриптов,
# которые уже перешли в корень репозитория (docker compose доступен).

API="${API_URL:-http://127.0.0.1:8080}"
PROM="${PROM_URL:-http://127.0.0.1:9090}"
NATS_MON="${NATS_MON_URL:-http://127.0.0.1:8222}"

ts() { date '+%H:%M:%S'; }
log() { echo "[$(ts)] $*"; }

# http_code <url> — код ответа (curl печатает 000 при недоступности/таймауте).
http_code() { curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$1" 2>/dev/null || true; }

# probe_api <метка> — срез кодов основных эндпоинтов одной строкой.
probe_api() {
  local label="${1:-}"
  local suggest details readyz
  suggest=$(http_code "$API/api/v1/suggest?q=пер&limit=10")
  details=$(http_code "$API/api/v1/addresses/101")
  readyz=$(http_code "$API/readyz")
  log "PROBE${label:+ [$label]}: suggest=$suggest details/101=$details api/readyz=$readyz"
}

# wait_healthy <compose-service> <timeout-sec> — ждёт статус (healthy).
# Возвращает число секунд ожидания; при таймауте — код 1.
wait_healthy() {
  local svc="$1" timeout="$2" waited=0 name status
  name=$(docker compose ps -q "$svc" 2>/dev/null || true)
  [ -n "$name" ] || { log "wait_healthy: сервис $svc не найден"; return 1; }
  while [ "$waited" -lt "$timeout" ]; do
    status=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$name" 2>/dev/null || echo unknown)
    if [ "$status" = "healthy" ] || [ "$status" = "running" ]; then
      echo "$waited"
      return 0
    fi
    sleep 2
    waited=$((waited + 2))
  done
  return 1
}

# wait_http_200 <url> <timeout-sec> — ждёт первый ответ 200; возвращает секунды.
wait_http_200() {
  local url="$1" timeout="$2" waited=0 code
  while [ "$waited" -lt "$timeout" ]; do
    code=$(http_code "$url")
    if [ "$code" = "200" ]; then
      echo "$waited"
      return 0
    fi
    sleep 1
    waited=$((waited + 1))
  done
  return 1
}

# jsz_pending <consumer> — число pending-сообщений консьюмера потока GAR.
jsz_pending() {
  local consumer="$1"
  curl -s --max-time 5 "$NATS_MON/jsz?consumers=true" | python3 -c "
import json, sys
try:
    data = json.load(sys.stdin)
    for acc in data.get('account_details', []):
        for stream in acc.get('stream_detail', []):
            for c in stream.get('consumer_detail', []):
                if c.get('name') == '$consumer':
                    print(c.get('num_pending', 0), c.get('num_redelivered', 0))
                    sys.exit(0)
except Exception:
    pass
print('-1 -1')
"
}

# prom_scalar <promql> — мгновенное скалярное значение из Prometheus.
prom_scalar() {
  curl -s --max-time 5 --get "$PROM/api/v1/query" --data-urlencode "query=$1" | python3 -c "
import json, sys
try:
    r = json.load(sys.stdin)['data']['result']
    print(r[0]['value'][1] if r else 'n/a')
except Exception:
    print('n/a')
"
}

# indexer_metric <result> — значение indexer_documents_indexed_total.
indexer_docs_indexed() { prom_scalar 'sum(indexer_documents_indexed_total)'; }
