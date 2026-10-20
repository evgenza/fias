#!/usr/bin/env bash
# smoke.sh — сквозная проверка платформы после `docker compose up -d --wait`:
# health, событийный конвейер на фикстуре, REST (HTTP/2), gRPC, метрики.
# Использование: scripts/smoke.sh [--with-seed]
#   --with-seed  сгенерировать фикстуру и прогнать полный конвейер импорта.
# На свежеподнятом стеке (CI) готовность и targets Prometheus могут отставать,
# поэтому ключевые проверки идут с ретраями (SMOKE_WAIT секунд на проверку).
set -euo pipefail
cd "$(dirname "$0")/.."

WITH_SEED="${1:-}"
FAILURES=0
SMOKE_WAIT="${SMOKE_WAIT:-60}"        # бюджет ожидания на одну проверку
SCRAPE_WAIT="${SCRAPE_WAIT:-120}"     # бюджет на появление targets в Prometheus
INDEX_WAIT="${INDEX_WAIT:-180}"       # бюджет на индексацию фикстуры

check() { # check <описание> <команда...>
  local desc="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "PASS: $desc"
  else
    echo "FAIL: $desc" >&2
    FAILURES=$((FAILURES + 1))
  fi
}

check_wait() { # check_wait <секунды> <описание> <команда...>
  local budget="$1" desc="$2"; shift 2
  local deadline=$((SECONDS + budget))
  until "$@" >/dev/null 2>&1; do
    if [ "$SECONDS" -ge "$deadline" ]; then
      echo "FAIL: $desc (не дождались за ${budget}s)" >&2
      FAILURES=$((FAILURES + 1))
      return
    fi
    sleep 3
  done
  echo "PASS: $desc"
}

echo "==> состояние контейнеров"
docker compose ps

echo "==> health/readiness"
check_wait "$SMOKE_WAIT" "address-api /healthz"   curl -sf http://127.0.0.1:8080/healthz
check_wait "$SMOKE_WAIT" "address-api /readyz"    curl -sf http://127.0.0.1:8080/readyz
check_wait "$SMOKE_WAIT" "suggester /readyz"      curl -sf http://127.0.0.1:9213/readyz
check_wait "$SMOKE_WAIT" "indexer /readyz"        curl -sf http://127.0.0.1:9212/readyz
check_wait "$SMOKE_WAIT" "importer /readyz"       curl -sf http://127.0.0.1:9211/readyz
check_wait "$SMOKE_WAIT" "elasticsearch"          curl -sf http://127.0.0.1:9200
check_wait "$SMOKE_WAIT" "prometheus ready"       curl -sf http://127.0.0.1:9090/-/ready
check_wait "$SMOKE_WAIT" "grafana health"         curl -sf http://127.0.0.1:3000/api/health
check_wait "$SMOKE_WAIT" "minio live"             curl -sf http://127.0.0.1:9000/minio/health/live

echo "==> REST API"
check "openapi.yaml"                curl -sf http://127.0.0.1:8080/openapi.yaml
check "swagger ui"                  curl -sf http://127.0.0.1:8080/swagger/index.html
check "web client"                  curl -sf http://127.0.0.1:8080/
check "HTTP/2 prior knowledge"      curl -sf --http2-prior-knowledge http://127.0.0.1:8080/healthz
check "suggest 400 на пустой q"     test "$(curl -s -o /dev/null -w '%{http_code}' 'http://127.0.0.1:8080/api/v1/suggest')" = "400"

echo "==> prometheus targets (приложения)"
target_up() { # target_up <job>
  curl -sf 'http://127.0.0.1:9090/api/v1/targets?state=active' \
    | jq -e --arg j "$1" '.data.activeTargets[] | select(.labels.job==$j and .health=="up")' >/dev/null
}
for job in importer indexer suggester address-api; do
  check_wait "$SCRAPE_WAIT" "target $job UP" target_up "$job"
done

if [ "$WITH_SEED" = "--with-seed" ]; then
  echo "==> полный конвейер: фикстура -> MinIO -> NATS -> PostgreSQL -> Elasticsearch"
  mkdir -p build
  go run ./cmd/gar-mkfixture --out build/tiny-gar.zip
  scripts/seed-demo.sh build/tiny-gar.zip
  echo "==> ожидание индексации (до ${INDEX_WAIT}s)"
  COUNT=""
  deadline=$((SECONDS + INDEX_WAIT))
  while [ "$SECONDS" -lt "$deadline" ]; do
    COUNT="$(curl -s http://127.0.0.1:9200/gar-addresses/_count 2>/dev/null | grep -o '"count":[0-9]*' | cut -d: -f2 || true)"
    [ "${COUNT:-0}" -gt 0 ] && break
    sleep 5
  done
  echo "    документов в gar-addresses: ${COUNT:-0}"
  check "индекс наполнен" test "${COUNT:-0}" -gt 0
  check_wait "$SMOKE_WAIT" "suggest находит улицу фикстуры" bash -c "curl -sf 'http://127.0.0.1:8080/api/v1/suggest?q=первая' | grep -qi 'первая'"
fi

if [ "$FAILURES" -gt 0 ]; then
  echo "==> SMOKE FAILED: $FAILURES проверок упало" >&2
  exit 1
fi
echo "==> SMOKE OK"
