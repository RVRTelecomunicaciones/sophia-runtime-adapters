package outbound_test

import (
	"testing"
)

// TestAdapterPort_PackageCompiles verifies the adapter.go port file compiles
// correctly. The Adapter interface itself is a pure declaration; behavioural
// tests live in testdoubles/testdoubles_test.go via StubAdapter.
func TestAdapterPort_PackageCompiles(t *testing.T) {
	// Intentionally empty — compilation is the test.
}
