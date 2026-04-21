package git

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// ErrUnsupportedAuth is returned when the requested AuthMode is not
// supported in Phase 1.
var ErrUnsupportedAuth = errors.New("unsupported auth mode")

// buildAuth constructs the go-git auth method for the given mode.
// Phase 1 supports only "none" (no credentials, anonymous public repo)
// and "ssh-agent" (defers to the local SSH agent — SSH_AUTH_SOCK must
// be populated). Embedded credentials are NEVER accepted (D8.3).
func (c Config) buildAuth(mode AuthMode) (transport.AuthMethod, error) {
	switch mode {
	case "", AuthNone:
		return nil, nil
	case AuthSSHAgent:
		if !c.SSHAgent {
			return nil, fmt.Errorf("%w: ssh-agent disabled in adapter config", ErrUnsupportedAuth)
		}
		if os.Getenv("SSH_AUTH_SOCK") == "" {
			return nil, fmt.Errorf("%w: SSH_AUTH_SOCK is unset", ErrUnsupportedAuth)
		}
		auth, err := ssh.NewSSHAgentAuth("git")
		if err != nil {
			return nil, fmt.Errorf("ssh-agent auth: %w", err)
		}
		return auth, nil
	default:
		return nil, fmt.Errorf("%w: %q (Phase 1 supports none|ssh-agent)", ErrUnsupportedAuth, mode)
	}
}
