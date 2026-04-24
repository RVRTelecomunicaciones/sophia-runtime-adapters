// ops/load/lib/common.js
//
// Shared helpers for k6 scenarios. Every scenario file imports from
// here rather than duplicating payload shapes or correlation-id
// generation. Keeps contracts in one place.
//
// Modern k6/execution API — D2C2.19. Never use __ITER / __VU globals.

import exec     from 'k6/execution';
import crypto   from 'k6/crypto';
import encoding from 'k6/encoding';
import http     from 'k6/http';
import { check } from 'k6';

// ---- endpoints -----------------------------------------------------------

export const baseURL = __ENV.RUNTIME_URL || 'http://runtime-adapters:8080';
// No trailing slash — symmetric with baseURL; scenario tags use the bare host.
export const mockURL = __ENV.MOCK_URL    || 'http://http-upstream-mock:8080';

export const defaultHeaders = {
    'Content-Type': 'application/json',
};

// ---- ID generation -------------------------------------------------------
//
// The runtime's shared.NewCorrelationID / NewIdempotencyKey validators
// (internal/domain/shared/ids.go) are STRICT: both fields require a
// 26-char Crockford base-32 ULID (alphabet excludes I/L/O/U, uppercase).
// A previous draft of this file used a prefixed hex scheme — the runtime
// rejected every request with 400 "invalid id: expected 26-char ULID".
// k6 doesn't ship a ULID library; we generate one in pure JS:
//   - 10 chars of 48-bit millisecond timestamp (Crockford base-32)
//   - 16 chars of 80-bit randomness (Crockford base-32)
// 80 random bits per iteration is plenty for bench-scale uniqueness.

const ULID_ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

function encodeTime(ms, len) {
    // ms fits in 48 bits, well under Number.MAX_SAFE_INTEGER (2^53).
    // Using / and % keeps it inside Number; bitwise ops would overflow.
    let out = '';
    let n = ms;
    for (let i = 0; i < len; i++) {
        out = ULID_ALPHABET[n % 32] + out;
        n = Math.floor(n / 32);
    }
    return out;
}

function encodeRandom16() {
    // 10 bytes = 80 bits = exactly 16 base-32 chars (no padding).
    // k6/crypto.randomBytes returns an ArrayBuffer.
    const bytes = new Uint8Array(crypto.randomBytes(10));
    let out = '';
    let acc = 0, bits = 0;
    for (let i = 0; i < bytes.length; i++) {
        acc = (acc << 8) | bytes[i];
        bits += 8;
        while (bits >= 5) {
            bits -= 5;
            out += ULID_ALPHABET[(acc >> bits) & 0x1F];
        }
        // acc stays under 2^12 between iterations; no overflow risk.
        acc &= (1 << bits) - 1;
    }
    return out;
}

function newULID() {
    return encodeTime(Date.now(), 10) + encodeRandom16();
}

export function newCorrelationID() {
    // Fresh ULID per request — lexicographically orderable, traceable.
    return newULID();
}

export function newIdempotencyKey() {
    // One per iteration — idempotency replays are NOT what baseline
    // measures; we want each iteration to hit the full flow. A fresh
    // ULID per iteration satisfies both (a) the runtime's strict
    // validator and (b) the desire for non-replaying traffic.
    return newULID();
}

// randomHex8 is an 8-char hex string used ONLY for filesystem path
// uniqueness (e.g. write-<iter>-<rand>.bin). Not for IDs — ID fields
// must go through newULID() above to satisfy the runtime validator.
export function randomHex8() {
    const bytes = new Uint8Array(crypto.randomBytes(4));
    let h = '';
    for (let i = 0; i < bytes.length; i++) {
        h += bytes[i].toString(16).padStart(2, '0');
    }
    return h;
}

// ---- payload builders ----------------------------------------------------
//
// IMPORTANT: payload field names below are taken directly from the Go
// adapter types under internal/adapters/outbound/<adapter>/types.go.
// Every adapter uses DisallowUnknownFields, so any extra or misnamed
// key produces an immediate 4xx parse failure (no measurement signal).
// When in doubt, grep the corresponding `json:"..."` tags in the
// adapter's types.go before editing.

export function payloadForShellExec() {
    // shell.exec@v1 — see internal/adapters/outbound/shell/types.go.
    // Deterministic, cheap: `echo ok`. Hits allowlist, no IO stall.
    return {
        command: '/bin/echo',
        args:    ['ok'],
        env:     {},
    };
}

// Pairs with capability `filesystem.read_file@v1`. The "Read" suffix
// is shorthand for "ReadFile" — kept short for caller ergonomics.
export function payloadForFilesystemRead(size) {
    // filesystem.read_file@v1 — see internal/adapters/outbound/filesystem/types.go.
    // size: '1kb' | '10kb' — caller alternates via
    // exec.scenario.iterationInTest % 2.
    const path = size === '10kb'
        ? '/tmp/bench-fs/read-10kb.bin'
        : '/tmp/bench-fs/read-1kb.bin';
    return { path };
}

// Pairs with capability `filesystem.write_file@v1`. The "Write" suffix
// is shorthand for "WriteFile" — kept short for caller ergonomics.
export function payloadForFilesystemWrite() {
    // filesystem.write_file@v1 — see internal/adapters/outbound/filesystem/types.go.
    // Wire fields: { path, data (base64), overwrite?, create_dirs?, mode? }.
    // Fresh file per iteration to avoid write conflicts / fs cache effects.
    const i = exec.scenario.iterationInTest;
    return {
        path:      `/tmp/bench-fs/write-${i}-${randomHex8()}.bin`,
        // 1 KiB fixed payload, base64-encoded as Data on the wire.
        data:      encoding.b64encode('x'.repeat(1024)),
        overwrite: true,
    };
}

export function payloadForHTTPRequest() {
    // http.request@v1 — see internal/adapters/outbound/httpreq/types.go.
    // No `timeout` field on the payload; envelope timeout_budget_ms governs.
    return {
        method:  'GET',
        url:     mockURL,
        headers: {},
    };
}

export function payloadForGitStatus(tree) {
    // git.status@v1 — see internal/adapters/outbound/git/types.go (StatusPayload).
    // tree: 'small-repo' | 'dirty-tree' — caller alternates.
    return { repo_path: `/bench/git/${tree}` };
}

export function payloadForGitClone(iteration) {
    // git.clone@v1 — see internal/adapters/outbound/git/types.go (ClonePayload).
    // file:// URL only — D2C2.9. Unique destination_path per iteration so
    // clones don't collide. Wire fields: { repo_url, destination_path, ref?, depth?, auth_mode? }.
    return {
        repo_url:         'file:///bench/git/small-repo',
        destination_path: `/tmp/bench-git-clone-${iteration}`,
        ref:              'HEAD',
    };
}

export function payloadForGitDiff() {
    // git.diff@v1 — see internal/adapters/outbound/git/types.go (DiffPayload).
    return {
        repo_path: '/bench/git/small-repo',
        from:      'HEAD~1',
        to:        'HEAD',
    };
}

export function payloadForGitCommit(workdir) {
    // git.commit@v1 — see internal/adapters/outbound/git/types.go (CommitPayload).
    // workdir is a fresh tmpfs copy prepared by the scenario's setup phase.
    return {
        repo_path:    workdir,
        message:      'bench commit',
        author_name:  'Bench',
        author_email: 'bench@localhost',
    };
}

// ---- execute-request wrapper --------------------------------------------

// splitCapability parses the canonical `<adapter>.<name>@<version>`
// form (e.g. "shell.exec@v1") into the three envelope fields. Scenarios
// pass the canonical string for readability; the runtime's HTTP envelope
// (see internal/domain/execution/entities/request.go UnmarshalJSON)
// requires adapter_id / capability_name / capability_version separately.
function splitCapability(canonical) {
    const atIdx = canonical.lastIndexOf('@');
    if (atIdx <= 0) {
        throw new Error(`invalid capability ${canonical}: missing @version`);
    }
    const [path, version] = [canonical.slice(0, atIdx), canonical.slice(atIdx + 1)];
    const dotIdx = path.indexOf('.');
    if (dotIdx <= 0) {
        throw new Error(`invalid capability ${canonical}: missing .name`);
    }
    return {
        adapter_id:         path.slice(0, dotIdx),
        capability_name:    path.slice(dotIdx + 1),
        capability_version: version,
    };
}

export function executeRequest(capability, payload, tags) {
    const parts = splitCapability(capability);
    const body = JSON.stringify({
        correlation_id:     newCorrelationID(),
        adapter_id:         parts.adapter_id,
        capability_name:    parts.capability_name,
        capability_version: parts.capability_version,
        payload:            payload,
        timeout_budget_ms:  30000,
        idempotency_key:    newIdempotencyKey(),
    });
    const res = http.post(`${baseURL}/api/v1/execute`, body, {
        headers: defaultHeaders,
        tags:    tags || {},
    });
    check(res, {
        'status is 200':    (r) => r.status === 200,
        'receipt_id present': (r) => {
            try { return r.json('receipt_id') !== null; } catch (e) { return false; }
        },
        'result.status in enum': (r) => {
            try {
                return ['success', 'failure', 'cancelled', 'timeout', 'partial']
                    .includes(r.json('result.status'));
            } catch (e) { return false; }
        },
    });
    return res;
}
