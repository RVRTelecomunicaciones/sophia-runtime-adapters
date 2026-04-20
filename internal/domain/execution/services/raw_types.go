// Package services holds domain services — stateless, side-effect-free
// dispatchers and policies applied to domain types. In Phase 1 the only
// service is ResultNormalizer (D3.12).
package services

// AdapterRawOutcome is the marker interface that every adapter-specific
// raw outcome type must implement. Adapters in Bundle 4 implement this
// with a single empty method; the type-identity check in ResultNormalizer
// is enforcement that the value carries an adapter-declared raw outcome
// rather than an arbitrary any.
//
// Concrete implementations (e.g. shell.execRaw, git.statusRaw) provide
// this marker with an empty body — see Bundle 4.
//
// Design note (D3.5, R5): raw outcomes live inside adapter packages.
// ResultNormalizer dispatches by canonical capability string without
// importing any adapter package, avoiding a domain→adapter circular
// import. Normalizer closures receive the raw value and type-assert to
// their own concrete type.
type AdapterRawOutcome interface {
	IsAdapterRawOutcome()
}
