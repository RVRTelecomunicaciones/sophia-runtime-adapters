// ops/load/scenarios/git_rough.js
//
// Rough tier (D2C2.1) — observation only. These 3 capabilities do NOT
// recalibrate targets in 2C.2. Report documents ranges but the YAML
// stays PROVISIONAL / ROUGH (D2C2.9 for clone).
//
// No thresholds. Scenarios are short and shape-preserving, not
// measurement-grade. Their purpose is "run the adapter once under
// bench-like conditions, document what we saw, no target decision".

import exec     from 'k6/execution';
import http     from 'k6/http';
import encoding from 'k6/encoding';
import { executeRequest,
         payloadForGitClone,
         payloadForGitDiff,
         payloadForGitCommit,
         baseURL,
         defaultHeaders,
         newCorrelationID,
         newIdempotencyKey } from '../lib/common.js';

export const options = {
    scenarios: {
        git_clone: {
            // Sequential 1 VU; clones are heavy. F (2C.4) bumps
            // iterations 20 → 60 for confident p99 (D2C4F.2). file://
            // only (D2C2.9); local path banned because git detects
            // local FS and uses hardlinks, giving artificially cheap
            // latency that doesn't represent real adapter behavior.
            executor: 'per-vu-iterations',
            vus: 1, iterations: 60, maxDuration: '15m',
            exec: 'cloneScenario', startTime: '0s',
            tags: { capability: 'git.clone@v1', tier: 'rough' },
        },
        git_diff: {
            executor: 'constant-arrival-rate',
            rate: 5, timeUnit: '1s', duration: '1m',
            preAllocatedVUs: 10, maxVUs: 20, gracefulStop: '10s',
            // Reschedule to follow bumped clone (was '5m10s').
            exec: 'diffScenario', startTime: '15m10s',
            tags: { capability: 'git.diff@v1', tier: 'rough' },
        },
        git_commit: {
            // Sequential 1 VU; commits mutate a tmpfs copy per
            // iteration. F (2C.4) bumps iterations 10 → 30 for
            // confident p99 (D2C4F.2).
            executor: 'per-vu-iterations',
            vus: 1, iterations: 30, maxDuration: '6m',
            // Reschedule to follow bumped clone + diff (was '6m25s').
            exec: 'commitScenario', startTime: '16m20s',
            tags: { capability: 'git.commit@v1', tier: 'rough' },
        },
    },
    // F (2C.4) — observation-only / instrumentation thresholds.
    //
    // These thresholds are NOT SLO targets — their primary function
    // is to force k6 to emit filtered sub-metrics in handleSummary
    // keyed by the `capability` and `tier` tags. Without a threshold
    // reference, k6's summary does NOT include per-tag p50/p95/p99
    // breakdowns, which means the calibration-report generator
    // cannot extract per-capability observed values (lesson from
    // ops/slo/calibration-reports/2026-04-25-baseline-v2.md:155).
    //
    // Values are deliberately ~10× expected p99 — high enough to
    // NEVER fail under normal conditions. They CAN fail if
    // performance catastrophically regresses (e.g., diff p99 jumps
    // from sub-100ms to >10s), and that is intentional: the run
    // would surface a pre-existing regression rather than silently
    // produce a calibration baseline against degraded behavior.
    thresholds: {
        'http_req_duration{capability:git.clone@v1,tier:rough}':  ['p(99)<60000'],
        'http_req_failed{capability:git.clone@v1,tier:rough}':    ['rate<1'],
        'http_req_duration{capability:git.diff@v1,tier:rough}':   ['p(99)<10000'],
        'http_req_failed{capability:git.diff@v1,tier:rough}':     ['rate<1'],
        'http_req_duration{capability:git.commit@v1,tier:rough}': ['p(99)<10000'],
        'http_req_failed{capability:git.commit@v1,tier:rough}':   ['rate<1'],
    },
    summaryTrendStats: ['min', 'avg', 'p(50)', 'p(95)', 'p(99)', 'max', 'count'],
};

// ---- scenario exec functions --------------------------------------------

export function cloneScenario() {
    executeRequest('git.clone@v1',
        payloadForGitClone(exec.scenario.iterationInTest),
        { capability: 'git.clone@v1' });
}

export function diffScenario() {
    executeRequest('git.diff@v1', payloadForGitDiff(),
        { capability: 'git.diff@v1' });
}

// For commit we need a fresh tmpfs workdir per iteration. We ask the
// runtime to do the clone first via file://, then commit into it.
// Alternative: mount a tmpfs and pre-seed — but simpler is to chain
// two runtime calls per iteration.
export function commitScenario() {
    const iter = exec.scenario.iterationInTest;
    const workdir = `/tmp/bench-git-commit-${exec.vu.idInTest}-${iter}`;

    // 1. Clone small-repo (file://) to a fresh path.
    //    ClonePayload (git/types.go): { repo_url, destination_path, ref?, ... }.
    const cloneBody = JSON.stringify({
        correlation_id:     newCorrelationID(),
        adapter_id:         'git',
        capability_name:    'clone',
        capability_version: 'v1',
        payload: {
            repo_url:         'file:///bench/git/small-repo',
            destination_path: workdir,
            ref:              'HEAD',
        },
        timeout_budget_ms:  30000,
        idempotency_key:    newIdempotencyKey(),
    });
    http.post(`${baseURL}/api/v1/execute`, cloneBody, { headers: defaultHeaders });

    // 2. Edit a file + commit.
    //    The runtime itself doesn't have a "write file to git workdir
    //    then commit" composition — git.commit@v1 expects a dirty
    //    working tree. So we apply a deterministic change via a
    //    separate filesystem.write_file call first.
    //    WriteFilePayload (filesystem/types.go): { path, data (base64), overwrite?, ... }.
    const writeBody = JSON.stringify({
        correlation_id:     newCorrelationID(),
        adapter_id:         'filesystem',
        capability_name:    'write_file',
        capability_version: 'v1',
        payload: {
            path:      `${workdir}/bench-edit.txt`,
            data:      encoding.b64encode(`bench iter ${iter}\n`),
            overwrite: true,
        },
        timeout_budget_ms:  10000,
        idempotency_key:    newIdempotencyKey(),
    });
    http.post(`${baseURL}/api/v1/execute`, writeBody, { headers: defaultHeaders });

    // 3. Commit the change — this is the measurement call.
    executeRequest('git.commit@v1', payloadForGitCommit(workdir),
        { capability: 'git.commit@v1', iter: iter.toString() });

    // Best-effort cleanup (runtime has no rm capability; we'd need
    // shell.exec with rm which is out of the allowlist). Left to
    // compose teardown: `docker compose down -v` removes the volume
    // where these tmp dirs live (they're inside the runtime
    // container's writable layer — ephemeral by default).
}
