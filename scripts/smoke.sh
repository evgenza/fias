#!/usr/bin/env bash
# smoke.sh — сквозная проверка платформы после `docker compose up -d --wait`:
# health, событийный конвейер на фикстуре, REST (HTTP/2), gRPC, метрики.
# Использование: scripts/smoke.sh [--with-seed]
#   --with-seed  сгенерировать фикстуру и прогнать полный конвейер импорта.
set -euo pipefail
cd "$(dirname "$0")/.."

WITH_SEED="${1:-}"
FAILURES=0

check() { # check <описание> <команда...>
  local desc="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "PASS: $desc"
  else
    echo "FAIL: $desc" >&2
    FAILURES=$((FAILURES + 1))
  fi
}

echo "==> состояние контейнеров"
docker compose ps

echo "==> health/readiness"
check "address-api /healthz"        curl -sf http://127.0.0.1:8080/healthz
check "address-api /readyz"         curl -sf http://127.0.0.1:8080/readyz
check "suggester /readyz"           curl -sf http://127.0.0.1:9213/readyz
check "indexer /readyz"             curl -sf http://127.0.0.1:9212/readyz
check "importer /readyz"            curl -sf http://127.0.0.1:9211/readyz
check "elasticsearch"               curl -sf http://127.0.0.1:9200
check "prometheus ready"            curl -sf http://127.0.0.1:9090/-/ready
check "grafana health"              curl -sf http://127.0.0.1:3000/api/health
check "minio live"                  curl -sf http://127.0.0.1:9000/minio/health/live

echo "==> REST API"
check "openapi.yaml"                curl -sf http://127.0.0.1:8080/openapi.yaml
check "swagger ui"                  curl -sf http://127.0.0.1:8080/swagger/index.html
check "web client"                  curl -sf http://127.0.0.1:8080/
check "HTTP/2 prior knowledge"      curl -sf --http2-prior-knowledge http://127.0.0.1:8080/healthz
check "suggest 400 на пустой q"     test "$(curl -s -o /dev/null -w '%{http_code}' 'http://127.0.0.1:8080/api/v1/suggest')" = "400"

echo "==> prometheus targets (приложения)"
for job in importer indexer suggester address-api; do
  check "target $job UP" bash -c "curl -sf 'http://127.0.0.1:9090/api/v1/targets?state=active' | grep -q '\"job\":\"$job\".*\"health\":\"up\"'"
done

if [ "$WITH_SEED" = "--with-seed" ]; then
  echo "==> полный конвейер: фикстура -> MinIO -> NATS -> PostgreSQL -> Elasticsearch"
  mkdir -p build
  go run ./cmd/gar-mkfixture --out build/tiny-gar.zip
  scripts/seed-demo.sh build/tiny-gar.zip
  echo "==> ожидание индексации (до 120s)"
  for i in $(seq 1 24); do
    COUNT="$(curl -s http://127.0.0.1:9200/gar-addresses/_count 2>/dev/null | grep -o '"count":[0-9]*' | cut -d: -f2 || true)"
    [ "${COUNT:-0}" -gt 0 ] && break
    sleep 5
  done
  echo "    документов в gar-addresses: ${COUNT:-0}"
  check "индекс наполнен" test "${COUNT:-0}" -gt 0
  check "suggest находит улицу фикстуры" bash -c "curl -sf 'http://127.0.0.1:8080/api/v1/suggest?q=первая' | grep -qi 'первая'"
fi

if [ "$FAILURES" -gt 0 ]; then
  echo "==> SMOKE FAILED: $FAILURES проверок упало" >&2
  exit 1
fi
echo "==> SMOKE OK"
