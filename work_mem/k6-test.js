import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const ENDPOINT = __ENV.ENDPOINT || '/low-work-mem';

export const options = {
  stages: [
    { duration: '15s', target: 10 }, // ramp up to 10 users
    { duration: '15s', target: 10 },
    { duration: '15s', target: 0 },  // ramp down to 0 users
  ],
};

export default function () {
  const res = http.get(`${BASE_URL}${ENDPOINT}`);
  check(res, { 'status is 200': (r) => r.status === 200 });
}
