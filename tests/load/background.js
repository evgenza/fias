// background.js — фоновая нагрузка для хаос-экспериментов.
// 50% suggest / 50% details, параметры через env:
//   VUS (по умолчанию 20), DURATION (по умолчанию 3m).
// Thresholds смягчены: во время хаоса ошибки ожидаемы и являются
// предметом измерения, а не поводом остановить прогон.
import http from 'k6/http';
import { sleep } from 'k6';
import { BASE_URL, randomQuery, randomId, checkSuggest, checkDetails, envDuration, envVus } from './lib.js';

export const options = {
  scenarios: {
    background: {
      executor: 'constant-vus',
      vus: envVus(20),
      duration: envDuration('3m'),
    },
  },
  thresholds: {
    http_req_failed: ['rate<1'],
  },
};

export default function () {
  if (Math.random() < 0.5) {
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
  sleep(0.2);
}
