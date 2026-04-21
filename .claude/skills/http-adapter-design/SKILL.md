---
name: http-adapter-design
description: Strict SSRF by default, TLS verify mandatory, redirect cap 5, status mapping per §8.5.
triggers:
  - "internal/adapters/outbound/httpreq/**/*.go"
---

# http-adapter-design

**When this skill applies**: Writing or reviewing the HTTP adapter (`httpreq` package, `AdapterID="http"`), its SSRF guard, or its HTTP status mapping.

## Rules

- **SSRF strict block by default** (A8.3): Before making any request, resolve the target hostname to IP(s) and block if any resolved address falls in: RFC 1918 (10/8, 172.16/12, 192.168/16), RFC 6598 (100.64/10), loopback (127/8, ::1), link-local (169.254/16, fe80::/10), or multicast (224/4, ff00::/8). This check applies to every redirect hop too.
- **`RUNTIME_HTTP_ALLOW_PRIVATE_NETWORKS=true` disables the SSRF block** (A8.3): Operators may opt in for internal network use. The default is `false`. Never skip the check without this flag set.
- **TLS verification is mandatory** (D8.3): `InsecureSkipVerify: true` is forbidden in production HTTP clients. No exceptions for "self-signed in dev" — use a trusted CA or opt-out env flag `RUNTIME_HTTP_SKIP_TLS_VERIFY=true` (operator responsibility).
- **Redirect cap default 5** (§8.4): Configure `http.Client.CheckRedirect` to return `http.ErrUseLastResponse` after 5 redirects. Re-apply the SSRF check on each redirect destination.
- **Status code mapping per §8.5** (D4.5):
  - 2xx → `ExecutionStatus=Success`
  - 4xx → `ExecutionStatus=Failed`, `ErrorClass=ExternalFailure`, `RetryHint=DoNotRetry`
  - 5xx → `ExecutionStatus=Failed`, `ErrorClass=ExternalFailure`, `RetryHint=Retryable`
  - Network/timeout errors → `ExecutionStatus=Failed`, `ErrorClass=Timeout` or `ErrorClass=NetworkFailure`
- **`AdapterID="http"`** (Gap-1): The external adapter ID is `"http"` even though the Go package is `httpreq`. All `CapabilityDescriptor` entries must use `AdapterID: "http"`.
- **Request timeout from context** (D9.1): Use the context deadline from the execution request — do not set an independent `http.Client.Timeout`. The context is the sole timeout mechanism.
- **Response body limited** (D8.5): Read the response body up to `MaxPayloadBytes` (1 MiB default). Excess triggers `ErrorClass=PayloadTooLarge`.

## Anti-patterns

- **Skipping SSRF check on redirect hops**: A redirect to `http://169.254.169.254/` (AWS metadata) bypasses a one-time check at request start.
- **`InsecureSkipVerify: true` hardcoded**: Creates an undetectable MITM risk in production; operators must opt out explicitly.
- **Using `http.Client.Timeout` instead of context**: Creates a second, uncoordinated timeout that ignores the runtime's effective budget.
- **Mapping 404 as `Retryable`**: Client errors (4xx) do not improve with retry — always `DoNotRetry`.
- **Using package name `httpreq` as `AdapterID`**: Callers identify adapters by `AdapterID`; the Go package name is an implementation detail.
