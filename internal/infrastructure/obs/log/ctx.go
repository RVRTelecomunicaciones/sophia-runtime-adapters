package log

import "context"

type ctxKey struct{}

// ContextWith returns a new ctx carrying the given logger.
func ContextWith(ctx context.Context, l *Logger) context.Context {
	if l == nil {
		l = NewNop()
	}
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext retrieves the Logger bound to ctx. Returns a Nop logger if
// none is bound, or if ctx is nil. NEVER returns nil (runtime invariant —
// obs must not crash the request path).
func FromContext(ctx context.Context) *Logger {
	if ctx == nil {
		return NewNop()
	}
	v := ctx.Value(ctxKey{})
	if v == nil {
		return NewNop()
	}
	l, ok := v.(*Logger)
	if !ok || l == nil {
		return NewNop()
	}
	return l
}
