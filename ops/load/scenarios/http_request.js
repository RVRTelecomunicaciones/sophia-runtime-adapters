// ops/load/scenarios/http_request.js
//
// Core capability: http.request@v1
// Scenarios: baseline (3m @ 30 rps) + saturation (4m ramp 100→500 rps)
//
// Target: http-upstream-mock inside the bridge network (nginx static
// ~1 KiB response, §6.8). The runtime's SSRF guards reject public
// and loopback addresses by default; the compose sets
// RUNTIME_HTTP_ALLOW_PRIVATE_NETWORKS=true and
// RUNTIME_HTTP_HOST_ALLOWLIST=http-upstream-mock.

import { executeRequest, payloadForHTTPRequest } from '../lib/common.js';

export const options = {
    scenarios: {
        baseline: {
            executor:         'constant-arrival-rate',
            rate:             30,
            timeUnit:         '1s',
            duration:         '3m',
            preAllocatedVUs:  30,
            maxVUs:           80,
            gracefulStop:     '15s',
            tags: { scenario: 'baseline', capability: 'http.request@v1', tier: 'core' },
        },
        saturation: {
            executor:         'ramping-arrival-rate',
            startRate:        50,
            timeUnit:         '1s',
            preAllocatedVUs:  80,
            maxVUs:           500,
            stages: [
                { target: 100, duration: '1m' },
                { target: 200, duration: '1m' },
                { target: 400, duration: '1m' },
                { target: 500, duration: '1m' },
            ],
            gracefulStop: '30s',
            startTime: '3m30s',
            tags: { scenario: 'saturation', capability: 'http.request@v1', tier: 'core' },
        },
    },
    thresholds: {
        'http_req_duration{scenario:baseline,capability:http.request@v1}': ['p(99)<10000'],
        'http_req_failed{scenario:baseline,capability:http.request@v1}':    ['rate<0.01'],
    },
    summaryTrendStats: ['min', 'avg', 'p(50)', 'p(95)', 'p(99)', 'max', 'count'],
};

export default function () {
    executeRequest('http.request@v1', payloadForHTTPRequest(),
        { capability: 'http.request@v1' });
}
