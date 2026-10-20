// details.js — нагрузочный тест карточек /api/v1/addresses/{id}.
// Постоянные 30 VU в течение 2 минут. Демонстрирует кэш Redis:
// по заголовку X-Cache (hit/miss) собирается кастомная метрика
// cache_hit_ratio (Trend) и счётчики cache_hits/cache_misses.
import http from 'k6/http';
import { sleep } from 'k6';
import { BASE_URL, randomId, checkDetails } from './lib.js';

export const options = {
  scenarios: {
    details_const: {
      executor: 'constant-vus',
      vus: 30,
      duration: '2m',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500'],
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
  },
};

export default function () {
  const id = randomId();
  const res = http.get(`${BASE_URL}/api/v1/addresses/${id}`, {
    tags: { endpoint: 'details' },
  });
  checkDetails(res);
  sleep(0.05);
}
