// ops/load/scenarios/git_status.js
//
// Smoke tier — git.status@v1 (D2C2.1). Two executors:
//   smoke            (5 rps × 90s)
//   saturation_lite  (ramp 20→80 rps × 3m)
//
// Alternates 50/50 between small-repo (clean) and dirty-tree
// (modified + untracked) via exec.scenario.iterationInTest % 2
// (D2C2.19). Evidence file git-status-smoke-split.json will
// carry the segmented metrics via `tree=` tag.

import exec from 'k6/execution';
import { executeRequest, payloadForGitStatus } from '../lib/common.js';

export const options = {
    scenarios: {
        smoke: {
            executor: 'constant-arrival-rate',
            rate: 5, timeUnit: '1s', duration: '90s',
            preAllocatedVUs: 10, maxVUs: 20, gracefulStop: '10s',
            tags: { capability: 'git.status@v1', tier: 'smoke' },
        },
        saturation_lite: {
            executor: 'ramping-arrival-rate',
            startRate: 20, timeUnit: '1s',
            preAllocatedVUs: 50, maxVUs: 150,
            stages: [
                { target: 40, duration: '1m' },
                { target: 80, duration: '2m' },
            ],
            gracefulStop: '30s',
            startTime: '1m35s',
            tags: { capability: 'git.status@v1', tier: 'smoke' },
        },
    },
    thresholds: {
        // Reflects PROVISIONAL target (p99<2s). Relaxed during smoke.
        'http_req_duration{phase:smoke,capability:git.status@v1}': ['p(99)<2000'],
        'http_req_failed{phase:smoke,capability:git.status@v1}':    ['rate<0.01'],
        // F (2C.4) per-tree instrumentation thresholds. NOT SLO
        // targets — their function is to force k6 to emit filtered
        // sub-metrics tagged by `tree=small-repo|dirty-tree` so the
        // calibration report can analyze whether dirty trees are
        // materially slower than clean ones. The runtime SLO for
        // git.status@v1 stays a SINGLE capability-keyed SLO (no
        // `tree` label on the runtime metric — R16 unchanged); the
        // per-tree split informs the threshold decision but does not
        // surface as a runtime label. See spec §4.2 + D2C4F.5.
        //
        // Loose values (~25× current SMOKE_CALIBRATED p99 of ~50ms)
        // — these CAN fail if a tree-shape regression appears,
        // deliberately surfacing degradation rather than calibrating
        // against it.
        'http_req_duration{capability:git.status@v1,tree:small-repo}': ['p(99)<5000'],
        'http_req_duration{capability:git.status@v1,tree:dirty-tree}': ['p(99)<5000'],
    },
    summaryTrendStats: ['min', 'avg', 'p(50)', 'p(95)', 'p(99)', 'max', 'count'],
};

export default function () {
    const tree = (exec.scenario.iterationInTest % 2 === 0) ? 'small-repo' : 'dirty-tree';
    executeRequest('git.status@v1', payloadForGitStatus(tree),
        { capability: 'git.status@v1', tree: tree });
}
