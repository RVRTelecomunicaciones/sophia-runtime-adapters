# ADR 0003 — chi router for HTTP transport

- **Status:** accepted
- **Date:** 2026-04-19
- **Deciders:** Russell Vergara
- **Context:** Phase 1 needs an HTTP router for three inbound endpoints (POST /execute, GET /capabilities, GET /receipts/{id}). Options: stdlib `net/http.ServeMux`, `gin`, `echo`, `chi`. Phase 1 values stdlib-friendliness, minimal dependencies, and idiomatic middleware composition over raw speed. The Phase 2 hardening bundle may introduce middleware (rate limiting, auth, per-capability scopes) that benefit from a composable router.
- **Options considered:**
  - Option A — stdlib `net/http.ServeMux` (Go 1.22+): improved path patterns, zero deps, limited middleware story.
  - Option B — `go-chi/chi/v5`: lightweight, idiomatic middleware chain, path params + groups, stdlib-compatible http.Handler; single dep.
  - Option C — `gin`/`echo`: feature-rich, heavier dependency surface, less idiomatic to compose.
- **Decision:** Use `github.com/go-chi/chi/v5` (plan P2). Chi is the minimal idiomatic choice that gives us composable middleware (RequestID, panic-recover) and subrouter groups (/api/v1/*) without pulling in a whole framework.
- **Consequences:**
  - One external HTTP dep; stdlib `net/http` remains the primary interface (chi registers http.Handler).
  - Middleware chain reads linearly: `RequestID → requestIDHeader → panicRecoverer → handler`.
  - Phase 2 middleware (auth, rate limit, OTel tracing) plugs in without refactor.
  - `panicRecoverer` is implemented in our package (not chi's) so the HTTPError envelope stays consistent.
- **Spec references:** P2 (plan), §6.1 endpoint table, R4 (adapters do not panic).
