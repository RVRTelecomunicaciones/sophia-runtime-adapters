package valueobjects

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// phase1Cap is a helper that constructs a Capability and fails the test on error.
func phase1Cap(t *testing.T, adapterStr, name, version string, allowsPartial bool, timeout time.Duration) Capability {
	t.Helper()
	aid, err := NewAdapterID(adapterStr)
	if err != nil {
		t.Fatalf("NewAdapterID(%q): %v", adapterStr, err)
	}
	c, err := NewCapability(aid, name, version, allowsPartial, timeout)
	if err != nil {
		t.Fatalf("NewCapability(%q, %q, %q): %v", adapterStr, name, version, err)
	}
	return c
}

func TestCapability_Phase1Combinations(t *testing.T) {
	type tc struct {
		adapter       string
		name          string
		version       string
		allowsPartial bool
		timeout       time.Duration
		wantCanon     string
	}
	cases := []tc{
		{"shell", "exec", "v1", false, 30 * time.Second, "shell.exec@v1"},
		{"git", "status", "v1", false, 10 * time.Second, "git.status@v1"},
		{"git", "clone", "v1", false, 120 * time.Second, "git.clone@v1"},
		{"git", "diff", "v1", true, 30 * time.Second, "git.diff@v1"},
		{"git", "commit", "v1", false, 30 * time.Second, "git.commit@v1"},
		{"filesystem", "read_file", "v1", false, 5 * time.Second, "filesystem.read_file@v1"},
		{"filesystem", "write_file", "v1", false, 5 * time.Second, "filesystem.write_file@v1"},
		{"http", "request", "v1", true, 60 * time.Second, "http.request@v1"},
	}
	for _, tc := range cases {
		t.Run(tc.wantCanon, func(t *testing.T) {
			c := phase1Cap(t, tc.adapter, tc.name, tc.version, tc.allowsPartial, tc.timeout)
			if got := c.Canonical(); got != tc.wantCanon {
				t.Errorf("Canonical() = %q, want %q", got, tc.wantCanon)
			}
			if c.Name() != tc.name {
				t.Errorf("Name() = %q, want %q", c.Name(), tc.name)
			}
			if c.Version() != tc.version {
				t.Errorf("Version() = %q, want %q", c.Version(), tc.version)
			}
			if c.AllowsPartial() != tc.allowsPartial {
				t.Errorf("AllowsPartial() = %v, want %v", c.AllowsPartial(), tc.allowsPartial)
			}
			if c.DefaultTimeout() != tc.timeout {
				t.Errorf("DefaultTimeout() = %v, want %v", c.DefaultTimeout(), tc.timeout)
			}
		})
	}
}

func TestCapability_NameValidation(t *testing.T) {
	aid, _ := NewAdapterID("shell")
	good := func(name string) {
		t.Helper()
		if _, err := NewCapability(aid, name, "v1", false, time.Second); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}
	bad := func(name string) {
		t.Helper()
		if _, err := NewCapability(aid, name, "v1", false, time.Second); err == nil {
			t.Errorf("expected %q to be invalid, got nil error", name)
		}
	}

	good("exec")
	good("read_file")
	good("shell.exec")
	good("ab")

	bad("")                      // empty
	bad("Exec")                  // uppercase
	bad("1exec")                 // leading digit
	bad("exec-cmd")              // hyphen
	bad(strings.Repeat("a", 65)) // too long (65 chars)
	bad("a")                     // too short (only 1 char, regex requires 2+)
}

func TestCapability_VersionValidation(t *testing.T) {
	aid, _ := NewAdapterID("shell")
	good := func(ver string) {
		t.Helper()
		if _, err := NewCapability(aid, "exec", ver, false, time.Second); err != nil {
			t.Errorf("expected version %q to be valid, got: %v", ver, err)
		}
	}
	bad := func(ver string) {
		t.Helper()
		if _, err := NewCapability(aid, "exec", ver, false, time.Second); err == nil {
			t.Errorf("expected version %q to be invalid, got nil error", ver)
		}
	}

	good("v1")
	good("v2")
	good("v10")

	bad("1")
	bad("v")
	bad("v1.0")
	bad("version1")
	bad("")
}

func TestCapability_DefaultTimeoutValidation(t *testing.T) {
	aid, _ := NewAdapterID("shell")

	if _, err := NewCapability(aid, "exec", "v1", false, 0); err == nil {
		t.Error("expected error for zero timeout, got nil")
	}
	if _, err := NewCapability(aid, "exec", "v1", false, -time.Second); err == nil {
		t.Error("expected error for negative timeout, got nil")
	}
}

func TestCapability_EqualityByValue(t *testing.T) {
	aid, _ := NewAdapterID("shell")
	c1, _ := NewCapability(aid, "exec", "v1", false, 30*time.Second)
	c2, _ := NewCapability(aid, "exec", "v1", false, 30*time.Second)
	if c1 != c2 {
		t.Error("two Capabilities with identical fields should be equal by value")
	}

	c3, _ := NewCapability(aid, "exec", "v1", true, 30*time.Second)
	if c1 == c3 {
		t.Error("Capabilities differing in allowsPartial should not be equal")
	}
}

// TestCapability_MarshalJSON_EmitsAllFields guards against the regression
// where Capability had unexported fields and no MarshalJSON: encoding/json
// silently emitted "{}" because it could not see the fields. The
// integration test TestBuildRuntime_EndToEndSmoke caught this when
// /api/v1/capabilities returned an array of empty objects. The wire
// shape uses snake_case and emits default_timeout as milliseconds (an
// integer) — symmetric with the request envelope's timeout_budget_ms.
func TestCapability_MarshalJSON_EmitsAllFields(t *testing.T) {
	aid, _ := NewAdapterID("shell")
	c, _ := NewCapability(aid, "exec", "v1", true, 30*time.Second)

	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, b)
	}

	want := map[string]any{
		"adapter_id":         "shell",
		"name":               "exec",
		"version":            "v1",
		"allows_partial":     true,
		"default_timeout_ms": float64(30_000),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %q = %#v, want %#v (full body: %s)", k, got[k], v, b)
		}
	}
	if len(got) != len(want) {
		t.Errorf("expected exactly %d fields, got %d: %s", len(want), len(got), b)
	}
}

// TestCapability_RoundTripJSON exercises the symmetric Unmarshal path
// so SDK callers (in-proc Go consumers) and external HTTP clients
// share the same wire contract. Deliberately uses a separate adapter +
// non-default timeout to ensure no field is hardcoded in the marshal.
func TestCapability_RoundTripJSON(t *testing.T) {
	aid, _ := NewAdapterID("git")
	original, _ := NewCapability(aid, "clone", "v1", false, 90*time.Second)

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Capability
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, b)
	}

	if decoded != original {
		t.Errorf("round-trip mismatch:\n  before = %+v\n  after  = %+v", original, decoded)
	}
}

// TestCapability_UnmarshalJSON_RejectsInvalid asserts that the validator
// in NewCapability is reused on the wire — a malformed payload must
// fail to decode rather than producing a zero-valued Capability that
// would later panic deeper in the runtime.
func TestCapability_UnmarshalJSON_RejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"bad adapter_id", `{"adapter_id":"BAD","name":"exec","version":"v1","allows_partial":false,"default_timeout_ms":1000}`},
		{"bad name", `{"adapter_id":"shell","name":"BAD","version":"v1","allows_partial":false,"default_timeout_ms":1000}`},
		{"bad version", `{"adapter_id":"shell","name":"exec","version":"1.0","allows_partial":false,"default_timeout_ms":1000}`},
		{"zero timeout", `{"adapter_id":"shell","name":"exec","version":"v1","allows_partial":false,"default_timeout_ms":0}`},
		{"negative timeout", `{"adapter_id":"shell","name":"exec","version":"v1","allows_partial":false,"default_timeout_ms":-1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c Capability
			if err := json.Unmarshal([]byte(tc.body), &c); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}
