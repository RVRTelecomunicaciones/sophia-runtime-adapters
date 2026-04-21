---
name: resilience-timeouts-retries
description: context.Context discipline, timeout/cancel semantics, retry_hint policy (runtime NEVER retries).
triggers:
  - "internal/application/**/*.go"
  - "internal/adapters/outbound/**/*.go"
  - "internal/infrastructure/**/*.go"
---

# resilience-timeouts-retries

**When this skill applies**: Any code that handles execution timeouts, cancellation, concurrency limits, or retry hint propagation.

## Rules

- **`context.Context` is the sole timeout/cancel mechanism** (D9.1): Never use `time.After` or `time.Sleep` for execution timeouts. Derive a child context with `context.WithTimeout` from the inbound request context. Pass it through every layer.
- **Effective timeout = `min(request, cap.default, MaxTimeoutBudget)`** (A4.1): Compute this in the application layer before calling the adapter. Never let an adapter compute its own timeout from scratch.
- **Runtime NEVER retries** (R6 / I21): `ExecutionService` executes once. On failure, it returns the result with the appropriate `RetryHint`. The caller decides whether to retry. No retry loop, no backoff, no jitter inside the runtime.
- **`RetryHint` is a signal, not a decision** (R6): Adapters populate `RetryHint` on `AdapterRawOutcome`; `ResultNormalizer` propagates it to `ExecutionResult`. It is advisory to the caller — the runtime acts on it zero times.
- **`MaxConcurrentExecutions` fast-reject — no queue** (A9.1 / I22): When the semaphore is full, the runtime immediately returns `ExecutionStatus=Failed`, `ErrorClass=RateLimited`. It does not queue the request, does not wait, does not shed load gradually.
- **Context cancellation must propagate to adapters** (D9.1): Application layer passes the derived context to the adapter's `Execute`. Adapters must check `ctx.Err()` at meaningful points (before I/O, in wait loops).
- **Deadline exceeded is `ErrorClass=Timeout`** (D4.5): When the context deadline is exceeded (adapter returns after `ctx.Done()`), normalize to `ErrorClass=Timeout`, `RetryHint=Retryable`.
- **Acquire semaphore before record, release after** (A9.1): The concurrency semaphore wraps the full execution — including persistence. Release only after the receipt is written.

## Anti-patterns

- **Retry loop inside `ExecutionService`**: Violates R6 unconditionally — even for "obviously retryable" errors. The retry contract belongs to the caller.
- **`time.After` for adapter timeout**: Creates an uncoordinated timer that outlives the context; leaks goroutines.
- **Queueing when semaphore is full**: Callers expect a fast reject (I22); queuing introduces unbounded latency and breaks the contract.
- **Ignoring `ctx.Err()` in adapter wait loops**: A subprocess or network call that doesn't check context runs past its deadline and returns a stale result.
- **Applying retry hint as a runtime decision**: Reading `RetryHint=Retryable` and looping internally violates R6; pass the hint out and stop.
