package valueobjects

import (
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
