// suggest.js — нагрузочный тест подсказок /api/v1/suggest.
// Ступенчатый рост: 0→10 VU (30s) → 50 VU (1m) → 100 VU (1m) → 0 (30s).
// Каждая итерация — один GET /api/v1/suggest со случайным запросом из корпуса.
import http from 'k6/http';
import { sleep } from 'k6';
import { BASE_URL, randomQuery, checkSuggest } from './lib.js';

export const options = {
  scenarios: {
    suggest_ramp: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 10 },
        { duration: '1m', target: 50 },
        { duration: '1m', target: 100 },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500'],
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
  },
};

export default function () {
  const q = randomQuery();
  const res = http.get(`${BASE_URL}/api/v1/suggest?q=${encodeURIComponent(q)}&limit=10`, {
    tags: { endpoint: 'suggest' },
  });
  checkSuggest(res);
  sleep(0.1);
}
