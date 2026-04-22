// Package log is the contract-bound contextual logger for the runtime
// execution layer. Every log emitted through this package carries a fixed
// set of fields derived from the active ExecutionRequest and
// ExecutionReceipt — no sprintf, no payload, no request_id as core field.
//
// See docs/logging.md for the full contract and
// docs/superpowers/specs/2026-04-21-phase-2c-observability-slos-design.md
// §5 for the authoritative design.
package log
