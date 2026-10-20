#!/usr/bin/env bash
# run.sh — прогон всех нагрузочных сценариев k6 через docker.
# Использование: tests/load/run.sh [сценарий...]   (по умолчанию: suggest details mixed)
# Результаты: tests/load/results/<name>-summary.json + <name>.txt (текстовый вывод).
set -euo pipefail
cd "$(dirname "$0")/../.."

K6_IMAGE="${K6_IMAGE:-grafana/k6:0.54.0}"
NETWORK="${K6_NETWORK:-gar-importer_default}"
BASE_URL="${BASE_URL:-http://address-api:8080}"
SCENARIOS=("${@:-suggest details mixed}")
# shellcheck disable=SC2206
[ $# -gt 0 ] && SCENARIOS=("$@") || SCENARIOS=(suggest details mixed)

mkdir -p tests/load/results
FAILED=0

for name in "${SCENARIOS[@]}"; do
  echo "==> $(date +%H:%M:%S) сценарий: $name (image $K6_IMAGE, сеть $NETWORK, цель $BASE_URL)"
  set +e
  docker run --rm -i \
    --network "$NETWORK" \
    -v "$PWD/tests/load:/tests" \
    -e BASE_URL="$BASE_URL" \
    "$K6_IMAGE" run \
    --summary-export "/tests/results/${name}-summary.json" \
    "/tests/${name}.js" 2>&1 | tee "tests/load/results/${name}.txt"
  rc=${PIPESTATUS[0]}
  set -e
  if [ "$rc" -ne 0 ]; then
    echo "!!! сценарий $name завершился с кодом $rc (thresholds не пройдены или ошибка)" >&2
    FAILED=1
  else
    echo "==> сценарий $name OK"
  fi
done

exit "$FAILED"
