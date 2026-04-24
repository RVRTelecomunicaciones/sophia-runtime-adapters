// ops/load/scenarios/filesystem_write_file.js
//
// Core capability: filesystem.write_file@v1
// Scenarios: baseline (3m @ 20 rps) + saturation (4m ramp 50→300 rps)
//
// Each iteration writes a fresh file to avoid fs cache / same-path
// contention. Files live in the bench-fs compose volume and are
// discarded on compose down -v.

import { executeRequest, payloadForFilesystemWrite } from '../lib/common.js';

export const options = {
    scenarios: {
        baseline: {
            executor:         'constant-arrival-rate',
            rate:             20,
            timeUnit:         '1s',
            duration:         '3m',
            preAllocatedVUs:  20,
            maxVUs:           60,
            gracefulStop:     '15s',
            tags: { scenario: 'baseline', capability: 'filesystem.write_file@v1', tier: 'core' },
        },
        saturation: {
            executor:         'ramping-arrival-rate',
            startRate:        30,
            timeUnit:         '1s',
            preAllocatedVUs:  60,
            maxVUs:           300,
            stages: [
                { target: 50,  duration: '1m' },
                { target: 100, duration: '1m' },
                { target: 200, duration: '1m' },
                { target: 300, duration: '1m' },
            ],
            gracefulStop: '30s',
            startTime: '3m30s',
            tags: { scenario: 'saturation', capability: 'filesystem.write_file@v1', tier: 'core' },
        },
    },
    thresholds: {
        'http_req_duration{scenario:baseline,capability:filesystem.write_file@v1}': ['p(99)<1000'],
        'http_req_failed{scenario:baseline,capability:filesystem.write_file@v1}':    ['rate<0.01'],
    },
    summaryTrendStats: ['min', 'avg', 'p(50)', 'p(95)', 'p(99)', 'max', 'count'],
};

export default function () {
    executeRequest('filesystem.write_file@v1', payloadForFilesystemWrite(),
        { capability: 'filesystem.write_file@v1' });
}
