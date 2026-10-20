#!/bin/sh
# One-shot инициализация Kibana: ждёт доступности /api/status и создаёт
# data view gar-logs-* (идемпотентно: существующий не дублируется).
set -eu

KIBANA_URL="${KIBANA_URL:-http://kibana:5601}"
DATAVIEW_TITLE="gar-logs-*"
DATAVIEW_NAME="GAR логи сервисов"
TIME_FIELD="@timestamp"
MAX_WAIT_SEC=180

echo "kibana-init: ждём Kibana на $KIBANA_URL (до ${MAX_WAIT_SEC}с)..."
waited=0
while [ "$waited" -lt "$MAX_WAIT_SEC" ]; do
  if curl -sf "$KIBANA_URL/api/status" | grep -q '"overall":{"level":"available"'; then
    echo "kibana-init: Kibana доступна"
    break
  fi
  sleep 5
  waited=$((waited + 5))
done
if [ "$waited" -ge "$MAX_WAIT_SEC" ]; then
  echo "kibana-init: ОШИБКА: Kibana не стала доступна за ${MAX_WAIT_SEC}с" >&2
  exit 1
fi

if curl -sf "$KIBANA_URL/api/data_views" -H 'kbn-xsrf: true' | grep -qF "\"title\":\"$DATAVIEW_TITLE\""; then
  echo "kibana-init: data view \"$DATAVIEW_TITLE\" уже существует — пропуск"
  exit 0
fi

echo "kibana-init: создаём data view \"$DATAVIEW_TITLE\"..."
http_code=$(curl -s -o /tmp/kibana-init-response.json -w '%{http_code}' \
  -X POST "$KIBANA_URL/api/data_views/data_view" \
  -H 'kbn-xsrf: true' \
  -H 'Content-Type: application/json' \
  -d "{\"data_view\":{\"title\":\"$DATAVIEW_TITLE\",\"name\":\"$DATAVIEW_NAME\",\"timeFieldName\":\"$TIME_FIELD\"}}")

case "$http_code" in
  200|201|409)
    echo "kibana-init: HTTP $http_code"
    ;;
  *)
    echo "kibana-init: ОШИБКА: HTTP $http_code" >&2
    cat /tmp/kibana-init-response.json >&2
    exit 1
    ;;
esac

# Финальная проверка: data view действительно есть.
if curl -sf "$KIBANA_URL/api/data_views" -H 'kbn-xsrf: true' | grep -qF "\"title\":\"$DATAVIEW_TITLE\""; then
  echo "kibana-init: data view \"$DATAVIEW_TITLE\" на месте"
  exit 0
fi
echo "kibana-init: ОШИБКА: data view не найден после создания" >&2
exit 1
