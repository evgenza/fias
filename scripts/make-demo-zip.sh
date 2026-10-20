#!/usr/bin/env bash
# make-demo-zip.sh — сборка усечённой выгрузки ГАР из полного gar_xml.zip:
# корневые общероссийские справочники + каталог одного региона.
#
# Использование:
#   scripts/make-demo-zip.sh [полный.zip] [регион] [выход.zip]
# По умолчанию: ~/Downloads/gar_xml.zip, регион 77, build/gar-demo.zip.
#
# Извлечение идёт по central directory ZIP (без полного перебора 57 ГБ),
# сжатие — быстрое (zip -1), чтобы не тратить часы на deflate.
set -euo pipefail

SRC="${1:-$HOME/Downloads/gar_xml.zip}"
REGION="${2:-77}"
OUT="${3:-build/gar-demo.zip}"

command -v unzip >/dev/null || { echo "нужен unzip" >&2; exit 1; }
command -v zip >/dev/null || { echo "нужен zip" >&2; exit 1; }
[ -f "$SRC" ] || { echo "нет файла: $SRC" >&2; exit 1; }

mkdir -p "$(dirname "$OUT")"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "==> инвентаризация $SRC"
# Определяем обёртку первого сегмента (gar_xml/ или gar_delta_xml/), если есть.
FIRST="$(unzip -Z1 "$SRC" | head -1)"
WRAP=""
case "$FIRST" in
  gar_xml/*|gar_delta_xml/*) WRAP="${FIRST%%/*}/" ;;
esac
[ -n "$WRAP" ] && echo "==> обёртка архива: $WRAP"

unzip -Z1 "$SRC" | grep -E "^${WRAP}(${REGION}/|AS_)" > "$TMP/list.txt"
TOTAL="$(wc -l < "$TMP/list.txt" | tr -d ' ')"
echo "==> файлов к извлечению: $TOTAL (регион $REGION + корневые справочники)"

i=0
while IFS= read -r name; do
  i=$((i + 1))
  [ $((i % 50)) -eq 0 ] && echo "    $i/$TOTAL"
  unzip -p -C "$SRC" "$name" > "$TMP/data/$name" 2>/dev/null || {
    mkdir -p "$TMP/data/$(dirname "$name")"
    unzip -p -C "$SRC" "$name" > "$TMP/data/$name"
  }
done < "$TMP/list.txt"

echo "==> упаковка $OUT"
( cd "$TMP/data" && zip -1 -q -r "$OLDPWD/$OUT" . )
ls -lh "$OUT"
echo "==> готово: $OUT"
