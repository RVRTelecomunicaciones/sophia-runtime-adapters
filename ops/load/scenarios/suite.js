// ops/load/scenarios/suite.js
//
// Single entrypoint for the full baseline run. Composes all core 4
// scenarios + git tiered scenarios sequentially. Total runtime
// ~42–45 min (core ~32m + git ~12.5–13.5m) — see §5.6 table.
//
// Sequential by design: concurrent runs would cross-contaminate
// capability measurements on a 2-CPU runtime container.
//
// We implement sequentiality via `startTime` offsets. The first
// capability starts at t=0; subsequent capabilities wait their turn
// by using cumulative start offsets that account for the previous
// scenario's full duration (baseline + gracefulStop + saturation +
// gracefulStop + safety gap).
//
// handleSummary(data) writes /summaries/summary.json (mounted volume)
// as the SOLE machine-readable output (D2C2.17 / A2C2.26).

import exec from 'k6/execution';

// Re-export each scenario's default function under a unique name so
// we can register them under different exec: hooks without collision.
import { executeRequest,
         payloadForShellExec,
         payloadForFilesystemRead,
         payloadForFilesystemWrite,
         payloadForHTTPRequest,
         payloadForGitStatus,
         payloadForGitClone,
         payloadForGitDiff,
         payloadForGitCommit } from '../lib/common.js';

// ---- exec functions per capability --------------------------------------

export function shellExec() {
    executeRequest('shell.exec@v1', payloadForShellExec(),
        { capability: 'shell.exec@v1' });
}

export function fsRead() {
    const size = (exec.scenario.iterationInTest % 2 === 0) ? '1kb' : '10kb';
    executeRequest('filesystem.read_file@v1', payloadForFilesystemRead(size),
        { capability: 'filesystem.read_file@v1', size: size });
}

export function fsWrite() {
    executeRequest('filesystem.write_file@v1', payloadForFilesystemWrite(),
        { capability: 'filesystem.write_file@v1' });
}

export function httpRequest() {
    executeRequest('http.request@v1', payloadForHTTPRequest(),
        { capability: 'http.request@v1' });
}

export function gitStatus() {
    const tree = (exec.scenario.iterationInTest % 2 === 0) ? 'small-repo' : 'dirty-tree';
    executeRequest('git.status@v1', payloadForGitStatus(tree),
        { capability: 'git.status@v1', tree: tree });
}

export function gitClone() {
    executeRequest('git.clone@v1',
        payloadForGitClone(exec.scenario.iterationInTest),
        { capability: 'git.clone@v1' });
}

export function gitDiff() {
    executeRequest('git.diff@v1', payloadForGitDiff(),
        { capability: 'git.diff@v1' });
}

export function gitCommit() {
    // STUB pending Bundle 4 (git fixtures + git_rough.js).
    // git.commit@v1 needs a clone+edit workdir; that workdir is
    // provisioned by git_rough.js's commitScenario which chains
    // clone -> filesystem.write_file -> git.commit. Bundle 3
    // ships only the wire skeleton — running suite.js today
    // produces non-existent-path failures for git.commit
    // iterations (no thresholds gate these, so the suite still
    // completes; data is informational only until Bundle 4).
    const workdir = `/tmp/bench-git-commit-${exec.vu.idInTest}`;
    executeRequest('git.commit@v1', payloadForGitCommit(workdir),
        { capability: 'git.commit@v1' });
}

// ---- timing budget (§5.6) -----------------------------------------------
//
// We chain scenarios via startTime. Each core capability occupies an
// 8m30s slot: 3m baseline + 30s gap + 4m saturation + 30s graceful + 30s
// SAFETY buffer. Without the safety buffer, a saturation request still
// in-flight at gracefulStop end (the runtime is 2-CPU; saturation ramps
// to 400-800 rps) overlaps the next capability's baseline window and
// contaminates its p99. The +30s buys a genuine quiet window between
// any in-flight tail of the previous saturation and the next baseline.
// Git gets a ~13-min block afterwards.

const CORE_SLOT = '8m30s';     // nominal; see per-scenario offsets below
// Cumulative offsets:
const T_SHELL        = '0s';
const T_FS_READ      = '8m30s';   // CORE_SLOT
const T_FS_WRITE     = '17m';     // 2 * CORE_SLOT
const T_HTTP         = '25m30s';  // 3 * CORE_SLOT
const T_GIT_STATUS_SMOKE   = '34m';      // 4 * CORE_SLOT
const T_GIT_STATUS_SAT     = '35m35s';   // 90s smoke + 5s gap (rough tier — small overlap with smoke graceful drain is tolerated)
const T_GIT_ROUGH_CLONE    = '38m35s';   // + 3m saturation_lite
const T_GIT_ROUGH_DIFF     = '43m45s';   // + 5m clone maxDuration + 10s gap
const T_GIT_ROUGH_COMMIT   = '45m';      // + 1m diff + 15s gap

// ---- options -------------------------------------------------------------

export const options = {
    scenarios: {
        // ---- core tier ----
        shell_exec_baseline: {
            executor: 'constant-arrival-rate',
            rate: 10, timeUnit: '1s', duration: '3m',
            preAllocatedVUs: 20, maxVUs: 50, gracefulStop: '15s',
            exec: 'shellExec', startTime: T_SHELL,
            tags: { capability: 'shell.exec@v1', tier: 'core' },
        },
        shell_exec_saturation: {
            executor: 'ramping-arrival-rate',
            startRate: 20, timeUnit: '1s',
            preAllocatedVUs: 50, maxVUs: 400,
            stages: [
                { target: 50, duration: '1m' }, { target: 100, duration: '1m' },
                { target: 200, duration: '1m' }, { target: 400, duration: '1m' },
            ],
            gracefulStop: '30s',
            exec: 'shellExec', startTime: '3m30s',
            tags: { capability: 'shell.exec@v1', tier: 'core' },
        },
        fs_read_baseline: {
            executor: 'constant-arrival-rate',
            rate: 50, timeUnit: '1s', duration: '3m',
            preAllocatedVUs: 30, maxVUs: 100, gracefulStop: '15s',
            exec: 'fsRead', startTime: T_FS_READ,
            tags: { capability: 'filesystem.read_file@v1', tier: 'core' },
        },
        fs_read_saturation: {
            executor: 'ramping-arrival-rate',
            startRate: 50, timeUnit: '1s',
            preAllocatedVUs: 100, maxVUs: 800,
            stages: [
                { target: 100, duration: '1m' }, { target: 200, duration: '1m' },
                { target: 400, duration: '1m' }, { target: 800, duration: '1m' },
            ],
            gracefulStop: '30s',
            exec: 'fsRead', startTime: '12m',      // T_FS_READ + 3m30s
            tags: { capability: 'filesystem.read_file@v1', tier: 'core' },
        },
        fs_write_baseline: {
            executor: 'constant-arrival-rate',
            rate: 20, timeUnit: '1s', duration: '3m',
            preAllocatedVUs: 20, maxVUs: 60, gracefulStop: '15s',
            exec: 'fsWrite', startTime: T_FS_WRITE,
            tags: { capability: 'filesystem.write_file@v1', tier: 'core' },
        },
        fs_write_saturation: {
            executor: 'ramping-arrival-rate',
            startRate: 30, timeUnit: '1s',
            preAllocatedVUs: 60, maxVUs: 300,
            stages: [
                { target: 50, duration: '1m' }, { target: 100, duration: '1m' },
                { target: 200, duration: '1m' }, { target: 300, duration: '1m' },
            ],
            gracefulStop: '30s',
            exec: 'fsWrite', startTime: '20m30s',  // T_FS_WRITE + 3m30s
            tags: { capability: 'filesystem.write_file@v1', tier: 'core' },
        },
        http_baseline: {
            executor: 'constant-arrival-rate',
            rate: 30, timeUnit: '1s', duration: '3m',
            preAllocatedVUs: 30, maxVUs: 80, gracefulStop: '15s',
            exec: 'httpRequest', startTime: T_HTTP,
            tags: { capability: 'http.request@v1', tier: 'core' },
        },
        http_saturation: {
            executor: 'ramping-arrival-rate',
            startRate: 50, timeUnit: '1s',
            preAllocatedVUs: 80, maxVUs: 500,
            stages: [
                { target: 100, duration: '1m' }, { target: 200, duration: '1m' },
                { target: 400, duration: '1m' }, { target: 500, duration: '1m' },
            ],
            gracefulStop: '30s',
            exec: 'httpRequest', startTime: '29m',  // T_HTTP + 3m30s
            tags: { capability: 'http.request@v1', tier: 'core' },
        },

        // ---- git.status smoke tier ----
        git_status_smoke: {
            executor: 'constant-arrival-rate',
            rate: 5, timeUnit: '1s', duration: '90s',
            preAllocatedVUs: 10, maxVUs: 20, gracefulStop: '10s',
            exec: 'gitStatus', startTime: T_GIT_STATUS_SMOKE,
            tags: { capability: 'git.status@v1', tier: 'smoke' },
        },
        git_status_saturation_lite: {
            executor: 'ramping-arrival-rate',
            startRate: 20, timeUnit: '1s',
            preAllocatedVUs: 50, maxVUs: 150,
            stages: [
                { target: 40, duration: '1m' }, { target: 80, duration: '2m' },
            ],
            gracefulStop: '30s',
            exec: 'gitStatus', startTime: T_GIT_STATUS_SAT,
            tags: { capability: 'git.status@v1', tier: 'smoke' },
        },

        // ---- git rough tier (observation only, no thresholds) ----
        git_clone_rough: {
            executor: 'per-vu-iterations',
            vus: 1, iterations: 20, maxDuration: '5m',
            exec: 'gitClone', startTime: T_GIT_ROUGH_CLONE,
            tags: { capability: 'git.clone@v1', tier: 'rough' },
        },
        git_diff_rough: {
            executor: 'constant-arrival-rate',
            rate: 5, timeUnit: '1s', duration: '1m',
            preAllocatedVUs: 10, maxVUs: 20, gracefulStop: '10s',
            exec: 'gitDiff', startTime: T_GIT_ROUGH_DIFF,
            tags: { capability: 'git.diff@v1', tier: 'rough' },
        },
        git_commit_rough: {
            executor: 'per-vu-iterations',
            vus: 1, iterations: 10, maxDuration: '4m',
            exec: 'gitCommit', startTime: T_GIT_ROUGH_COMMIT,
            tags: { capability: 'git.commit@v1', tier: 'rough' },
        },
    },
    thresholds: {
        // Core baseline: loose thresholds that echo PROVISIONAL targets.
        'http_req_duration{phase:baseline,capability:shell.exec@v1}':             ['p(99)<5000'],
        'http_req_duration{phase:baseline,capability:filesystem.read_file@v1}':   ['p(99)<500'],
        'http_req_duration{phase:baseline,capability:filesystem.write_file@v1}':  ['p(99)<1000'],
        'http_req_duration{phase:baseline,capability:http.request@v1}':            ['p(99)<10000'],
        // No thresholds for saturation or rough; those are observational.
    },
    summaryTrendStats: ['min', 'avg', 'p(50)', 'p(95)', 'p(99)', 'max', 'count'],
};

// handleSummary is the SOLE source of the machine-readable output
// (D2C2.17 / A2C2.26). --summary-export is NOT used.
export function handleSummary(data) {
    return {
        'stdout':              textSummary(data),
        '/summaries/summary.json': JSON.stringify(data, null, 2),
    };
}

// Minimal inline textSummary() so we don't depend on k6-jslib utils.
function textSummary(data) {
    const metrics = data.metrics;
    const pick = (name) => metrics[name]?.values ?? {};
    const fmt = (n) => (n === undefined ? '—' : (typeof n === 'number' ? n.toFixed(2) : String(n)));
    let out = '\n=== Baseline summary ===\n';
    const duration = pick('http_req_duration');
    const failed   = pick('http_req_failed');
    out += `http_req_duration  p50=${fmt(duration['p(50)'])}ms  p95=${fmt(duration['p(95)'])}ms  p99=${fmt(duration['p(99)'])}ms  count=${fmt(duration.count)}\n`;
    out += `http_req_failed    rate=${fmt(failed.rate)}  passes=${fmt(failed.passes)}  fails=${fmt(failed.fails)}\n`;
    out += `\nFull machine-readable summary: /summaries/summary.json\n`;
    return out;
}
