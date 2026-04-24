// ops/load/scenarios/filesystem_read_file.js
//
// Core capability: filesystem.read_file@v1
// Scenarios: baseline (3m @ 50 rps) + saturation (4m ramp 100→800 rps)
// Alternates between 1 KiB and 10 KiB reads via
// exec.scenario.iterationInTest % 2 (D2C2.19).
//
// Pre-requisite: compose brings up runtime with /tmp/bench-fs mounted
// (volume `bench-fs` in compose.yaml). The scenario's setup() creates
// the two seed files once.

import http     from 'k6/http';
import exec     from 'k6/execution';
import encoding from 'k6/encoding';
import { executeRequest, payloadForFilesystemRead, baseURL, defaultHeaders, newCorrelationID, newIdempotencyKey } from '../lib/common.js';

export const options = {
    scenarios: {
        baseline: {
            executor:         'constant-arrival-rate',
            rate:             50,
            timeUnit:         '1s',
            duration:         '3m',
            preAllocatedVUs:  30,
            maxVUs:           100,
            gracefulStop:     '15s',
            tags: { scenario: 'baseline', capability: 'filesystem.read_file@v1', tier: 'core' },
        },
        saturation: {
            executor:         'ramping-arrival-rate',
            startRate:        50,
            timeUnit:         '1s',
            preAllocatedVUs:  100,
            maxVUs:           800,
            stages: [
                { target: 100, duration: '1m' },
                { target: 200, duration: '1m' },
                { target: 400, duration: '1m' },
                { target: 800, duration: '1m' },
            ],
            gracefulStop: '30s',
            startTime: '3m30s',
            tags: { scenario: 'saturation', capability: 'filesystem.read_file@v1', tier: 'core' },
        },
    },
    thresholds: {
        'http_req_duration{scenario:baseline,capability:filesystem.read_file@v1}': ['p(99)<500'],
        'http_req_failed{scenario:baseline,capability:filesystem.read_file@v1}':    ['rate<0.01'],
    },
    summaryTrendStats: ['min', 'avg', 'p(50)', 'p(95)', 'p(99)', 'max', 'count'],
};

// setup() runs once before any VUs. We seed the two fixture files
// through the runtime's own filesystem.write_file@v1 to avoid needing
// an out-of-band mechanism. The files persist in the bench-fs volume
// across the entire k6 invocation.
export function setup() {
    const write = (path, size) => {
        const body = JSON.stringify({
            correlation_id:     newCorrelationID(),
            adapter_id:         'filesystem',
            capability_name:    'write_file',
            capability_version: 'v1',
            // WriteFilePayload (filesystem/types.go): { path, data (base64), overwrite?, ... }.
            payload: {
                path:      path,
                data:      encoding.b64encode('x'.repeat(size)),
                overwrite: true,
            },
            timeout_budget_ms:  10000,
            idempotency_key:    newIdempotencyKey(),
        });
        http.post(`${baseURL}/api/v1/execute`, body, { headers: defaultHeaders });
    };
    write('/tmp/bench-fs/read-1kb.bin',  1024);
    write('/tmp/bench-fs/read-10kb.bin', 10240);
    return {};
}

export default function () {
    const size = (exec.scenario.iterationInTest % 2 === 0) ? '1kb' : '10kb';
    executeRequest('filesystem.read_file@v1', payloadForFilesystemRead(size),
        { capability: 'filesystem.read_file@v1', size: size });
}
