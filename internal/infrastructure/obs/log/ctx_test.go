package log

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFromContext_MissingReturnsNop(t *testing.T) {
	got := FromContext(context.Background())
	require.NotNil(t, got, "FromContext must never return nil (contract)")
	// Nop logger's emit is safe:
	got.Info(context.Background(), "silent")
}

func TestFromContext_RoundTrip(t *testing.T) {
	want, _ := newMemLogger()
	ctx := ContextWith(context.Background(), want)
	got := FromContext(ctx)
	require.Same(t, want, got)
}

func TestFromContext_NilCtxDoesNotPanic(t *testing.T) {
	// nolint: staticcheck // deliberately test nil ctx
	got := FromContext(nil)
	require.NotNil(t, got)
}
