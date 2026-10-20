// lib.js — общие настройки нагрузочных тестов k6 для платформы ГАР.
// BASE_URL можно переопределить через env (по умолчанию — address-api
// внутри compose-сети gar-importer_default).
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

export const BASE_URL = __ENV.BASE_URL || 'http://address-api:8080';

// Корпус поисковых запросов для /api/v1/suggest.
// «моск» осознанно даёт ноль результатов по фикстуре региона 01 — это
// корректный ответ (200 с пустым items), а не ошибка.
export const SUGGEST_QUERIES = ['пер', 'фикс', 'первая', 'тест', 'моск'];

// Корпус object_id для /api/v1/addresses/{id}: все объекты фикстуры,
// реально существующие в PostgreSQL. id 305 исключён намеренно:
// такого объекта нет ни в БД, ни в индексе — API отвечает 404 NOT_FOUND.
export const DETAIL_IDS = [
  101, 102, 103, 104, 105, 106, // addr_obj
  201, 202, 203, 204, 205, // houses
  301, 302, 303, 304, // apartments
  306, // rooms (305 в фикстуре отсутствует)
  401, // stead
  501, // carplace
];

// Кастомные метрики: доля попаданий в кэш Redis по заголовку X-Cache.
export const cacheHits = new Counter('cache_hits');
export const cacheMisses = new Counter('cache_misses');
export const cacheHitRatio = new Trend('cache_hit_ratio');

export function randomQuery() {
  return SUGGEST_QUERIES[Math.floor(Math.random() * SUGGEST_QUERIES.length)];
}

export function randomId() {
  return DETAIL_IDS[Math.floor(Math.random() * DETAIL_IDS.length)];
}

// Проверка ответа suggest: 200, валидный JSON с полем items, быстрый ответ.
export function checkSuggest(res) {
  const ok = check(res, {
    'suggest: статус 200': (r) => r.status === 200,
    'suggest: json с items': (r) => {
      try {
        return Array.isArray(r.json().items);
      } catch (e) {
        return false;
      }
    },
    'suggest: < 500ms': (r) => r.timings.duration < 500,
  });
  return ok;
}

// Проверка ответа details: 200, валидный JSON с object_id; фиксация X-Cache.
export function checkDetails(res) {
  const ok = check(res, {
    'details: статус 200': (r) => r.status === 200,
    'details: json с object_id': (r) => {
      try {
        return r.json().object_id !== undefined;
      } catch (e) {
        return false;
      }
    },
    'details: < 500ms': (r) => r.timings.duration < 500,
  });
  const xc = (res.headers['X-Cache'] || '').toLowerCase();
  if (xc === 'hit') {
    cacheHits.add(1);
    cacheHitRatio.add(1);
  } else if (xc === 'miss') {
    cacheMisses.add(1);
    cacheHitRatio.add(0);
  }
  return ok;
}

// Параметры сценария из env (для фоновой нагрузки в хаос-тестах).
export function envDuration(def) {
  return __ENV.DURATION || def;
}

export function envVus(def) {
  return parseInt(__ENV.VUS || `${def}`, 10);
}
