// ops/load/scenarios/smoke.js
//
// CI advisory smoke — runs in GHA ubuntu-latest. Repo is public
// (4 CPU / 16 GiB runner) but compose.ci-smoke.yaml sizes its limits
// for the private-repo floor (2 CPU / 8 GiB) for robustness
// (D2C2.13). Core 4 only; reduced RPS (~10% of baseline); short
// durations; coarse threshold.
//
// Job is continue-on-error: true — this script is NEVER a gate.
// Its output (handleSummary → /summaries/smoke-summary.json) is
// consumed by ops/load/lib/ci-smoke-comment.sh to post a PR comment
// comparing deltas vs ops/slo/calibration-reports/latest-baseline.json.
//
// NOT used: __ITER/__VU (D2C2.19). NOT used: --summary-export (D2C2.17).

import exec     from 'k6/execution';
import http     from 'k6/http';
import encoding from 'k6/encoding';
import { executeRequest,
         payloadForShellExec,
         payloadForFilesystemRead,
         payloadForFilesystemWrite,
         payloadForHTTPRequest,
         newCorrelationID,
         newIdempotencyKey,
         baseURL,
         defaultHeaders } from '../lib/common.js';

// ---- exec functions ------------------------------------------------------

export function shellExec() {
    executeRequest('shell.exec@v1', payloadForShellExec(),
        { scenario: 'smoke', capability: 'shell.exec@v1' });
}
export function fsRead() {
    const size = (exec.scenario.iterationInTest % 2 === 0) ? '1kb' : '10kb';
    executeRequest('filesystem.read_file@v1', payloadForFilesystemRead(size),
        { scenario: 'smoke', capability: 'filesystem.read_file@v1', size: size });
}
export function fsWrite() {
    executeRequest('filesystem.write_file@v1', payloadForFilesystemWrite(),
        { scenario: 'smoke', capability: 'filesystem.write_file@v1' });
}
export function httpRequest() {
    executeRequest('http.request@v1', payloadForHTTPRequest(),
        { scenario: 'smoke', capability: 'http.request@v1' });
}

export function setup() {
    // Seed the 2 read files once (see filesystem_read_file.js setup()).
    const write = (path, size) => {
        const body = JSON.stringify({
            correlation_id:     newCorrelationID(),
            adapter_id:         'filesystem',
            capability_name:    'write_file',
            capability_version: 'v1',
            // WriteFilePayload (filesystem/types.go): { path, data (base64), overwrite?, ... }.
            payload: { path: path, data: encoding.b64encode('x'.repeat(size)), overwrite: true },
            timeout_budget_ms:  10000,
            idempotency_key:    newIdempotencyKey(),
        });
        http.post(`${baseURL}/api/v1/execute`, body, { headers: defaultHeaders });
    };
    write('/tmp/bench-fs/read-1kb.bin',  1024);
    write('/tmp/bench-fs/read-10kb.bin', 10240);
    return {};
}

export const options = {
    scenarios: {
        shell_exec: {
            executor: 'constant-arrival-rate',
            rate: 1, timeUnit: '1s', duration: '30s',
            preAllocatedVUs: 3, maxVUs: 10, gracefulStop: '5s',
            exec: 'shellExec', startTime: '0s',
            tags: { scenario: 'smoke', capability: 'shell.exec@v1' },
        },
        fs_read: {
            executor: 'constant-arrival-rate',
            rate: 5, timeUnit: '1s', duration: '30s',
            preAllocatedVUs: 10, maxVUs: 20, gracefulStop: '5s',
            exec: 'fsRead', startTime: '35s',
            tags: { scenario: 'smoke', capability: 'filesystem.read_file@v1' },
        },
        fs_write: {
            executor: 'constant-arrival-rate',
            rate: 2, timeUnit: '1s', duration: '30s',
            preAllocatedVUs: 5, maxVUs: 15, gracefulStop: '5s',
            exec: 'fsWrite', startTime: '1m10s',
            tags: { scenario: 'smoke', capability: 'filesystem.write_file@v1' },
        },
        http_request: {
            executor: 'constant-arrival-rate',
            rate: 3, timeUnit: '1s', duration: '30s',
            preAllocatedVUs: 10, maxVUs: 20, gracefulStop: '5s',
            exec: 'httpRequest', startTime: '1m45s',
            tags: { scenario: 'smoke', capability: 'http.request@v1' },
        },
    },
    thresholds: {
        // Coarse guard only — truly broken runtime would fail this.
        // Threshold violation does NOT fail the overall CI (the job
        // is continue-on-error: true in ci.yaml).
        'http_req_failed': ['rate<0.10'],
    },
    summaryTrendStats: ['min', 'avg', 'p(50)', 'p(95)', 'p(99)', 'max', 'count'],
};

// Sole source (D2C2.17) — handleSummary writes JSON for PR comment.
export function handleSummary(data) {
    return {
        'stdout': '[smoke complete — see PR comment]\n',
        '/summaries/smoke-summary.json': JSON.stringify(data, null, 2),
    };
}
