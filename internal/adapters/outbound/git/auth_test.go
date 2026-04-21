package git

import (
	"errors"
	"testing"
)

func TestBuildAuth_EmptyMode_ReturnsNilNil(t *testing.T) {
	c := Config{SSHAgent: false}
	auth, err := c.buildAuth("")
	if err != nil {
		t.Fatalf("buildAuth(''): unexpected error: %v", err)
	}
	if auth != nil {
		t.Errorf("buildAuth(''): expected nil auth, got %T", auth)
	}
}

func TestBuildAuth_AuthNone_ReturnsNilNil(t *testing.T) {
	c := Config{SSHAgent: true}
	auth, err := c.buildAuth(AuthNone)
	if err != nil {
		t.Fatalf("buildAuth(AuthNone): unexpected error: %v", err)
	}
	if auth != nil {
		t.Errorf("buildAuth(AuthNone): expected nil auth, got %T", auth)
	}
}

func TestBuildAuth_SSHAgent_DisabledInConfig_ReturnsErrUnsupportedAuth(t *testing.T) {
	c := Config{SSHAgent: false}
	_, err := c.buildAuth(AuthSSHAgent)
	if err == nil {
		t.Fatal("expected error when SSHAgent disabled in config, got nil")
	}
	if !errors.Is(err, ErrUnsupportedAuth) {
		t.Errorf("expected ErrUnsupportedAuth, got %v", err)
	}
}

func TestBuildAuth_SSHAgent_NoSSHAuthSock_ReturnsErrUnsupportedAuth(t *testing.T) {
	// Ensure SSH_AUTH_SOCK is unset for this test.
	t.Setenv("SSH_AUTH_SOCK", "")
	c := Config{SSHAgent: true}
	_, err := c.buildAuth(AuthSSHAgent)
	if err == nil {
		t.Fatal("expected error when SSH_AUTH_SOCK unset, got nil")
	}
	if !errors.Is(err, ErrUnsupportedAuth) {
		t.Errorf("expected ErrUnsupportedAuth, got %v", err)
	}
}

func TestBuildAuth_UnknownMode_ReturnsErrUnsupportedAuth(t *testing.T) {
	c := Config{SSHAgent: true}
	_, err := c.buildAuth("password")
	if err == nil {
		t.Fatal("expected error for unknown mode, got nil")
	}
	if !errors.Is(err, ErrUnsupportedAuth) {
		t.Errorf("expected ErrUnsupportedAuth, got %v", err)
	}
}

// TODO(T36): integration test for buildAuth(AuthSSHAgent) with a real
// SSH_AUTH_SOCK — requires a running SSH agent and is skipped in unit suite.
