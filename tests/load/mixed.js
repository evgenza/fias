// mixed.js — смешанная нагрузка: 80% suggest / 20% details,
// постоянные 50 VU в течение 2 минут. Максимально похоже на реальный
// паттерн пользователей веб-клиента.
import http from 'k6/http';
import { sleep } from 'k6';
import { BASE_URL, randomQuery, randomId, checkSuggest, checkDetails } from './lib.js';

export const options = {
  scenarios: {
    mixed: {
      executor: 'constant-vus',
      vus: 50,
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
  if (Math.random() < 0.8) {
    const res = http.get(`${BASE_URL}/api/v1/suggest?q=${encodeURIComponent(randomQuery())}&limit=10`, {
      tags: { endpoint: 'suggest' },
    });
    checkSuggest(res);
  } else {
    const res = http.get(`${BASE_URL}/api/v1/addresses/${randomId()}`, {
      tags: { endpoint: 'details' },
    });
    checkDetails(res);
  }
  sleep(0.1);
}
