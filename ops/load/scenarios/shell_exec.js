// ops/load/scenarios/shell_exec.js
//
// Core capability: shell.exec@v1
// Scenarios: baseline (3m @ 10 rps) + saturation (4m ramp 50→400 rps)
// Total runtime: ~8 min sequential.
//
// Run standalone:  k6 run ops/load/scenarios/shell_exec.js
// Run via suite:   k6 run ops/load/scenarios/suite.js

import { executeRequest, payloadForShellExec } from '../lib/common.js';

export const options = {
    scenarios: {
        baseline: {
            executor:         'constant-arrival-rate',
            rate:             10,
            timeUnit:         '1s',
            duration:         '3m',
            preAllocatedVUs:  20,
            maxVUs:           50,
            gracefulStop:     '15s',
            tags: { capability: 'shell.exec@v1', tier: 'core' },
        },
        saturation: {
            executor:         'ramping-arrival-rate',
            startRate:        20,
            timeUnit:         '1s',
            preAllocatedVUs:  50,
            maxVUs:           400,
            stages: [
                { target: 50,  duration: '1m' },
                { target: 100, duration: '1m' },
                { target: 200, duration: '1m' },
                { target: 400, duration: '1m' },
            ],
            gracefulStop: '30s',
            startTime: '3m30s',
            tags: { capability: 'shell.exec@v1', tier: 'core' },
        },
    },
    thresholds: {
        // Reflects current PROVISIONAL target (5s p99). Post-calibration
        // this tightens; but we leave it loose here so baseline gives
        // signal rather than fail-fast.
        'http_req_duration{phase:baseline,capability:shell.exec@v1}': ['p(99)<5000'],
        'http_req_failed{phase:baseline,capability:shell.exec@v1}':    ['rate<0.01'],
        // Saturation: no threshold. We WANT the breakpoint to show.
    },
    summaryTrendStats: ['min', 'avg', 'p(50)', 'p(95)', 'p(99)', 'max', 'count'],
};

export default function () {
    executeRequest('shell.exec@v1', payloadForShellExec(),
        { capability: 'shell.exec@v1' });
}
