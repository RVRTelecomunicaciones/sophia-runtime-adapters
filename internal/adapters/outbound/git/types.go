// Package git implements the Phase 1 git adapter. It exposes 4 capabilities
// (git.status@v1, git.clone@v1, git.diff@v1, git.commit@v1) — see T35..T38
// for the per-capability implementations. This file scaffolds the adapter
// shell + payload/raw type forward declarations.
//
// Library: github.com/go-git/go-git/v5 ONLY (D8.7 / ADR-0002). The adapter
// does NOT shell out to /usr/bin/git. As a consequence, client-side
// commit hooks are NEVER executed (A8.1) — documented limitation.
//
// External contract: AdapterID = "git".
package git

import (
	"time"
)

// Config bundles the bootstrap-loaded git adapter settings.
type Config struct {
	// AllowedWorkingDirs is the list of absolute roots within which a
	// repo path (status/diff/commit) or a clone destination must
	// resolve. Empty list → all paths rejected.
	AllowedWorkingDirs []string

	// SSHAgent enables SSH-agent auth for clone (and any future remote
	// op). When false, only "none" auth (anonymous, public repo) is
	// accepted. Embedded credentials are ALWAYS rejected (D8.3).
	SSHAgent bool

	// InlineStreamLimit caps stdout/stderr-equivalent stream bytes when
	// constructing entities.StreamRef in the per-capability normalizers.
	InlineStreamLimit int
}

// AuthMode is the wire enum for clone/fetch authentication.
type AuthMode string

const (
	AuthNone     AuthMode = "none"
	AuthSSHAgent AuthMode = "ssh-agent"
)

// Per-capability payload + raw outcome forward declarations. Concrete
// fields are populated in T35..T38; this scaffolding exists so adapter.go
// compiles and the dispatcher can route by capability name.

// StatusPayload — see T35.
type StatusPayload struct {
	RepoPath string `json:"repo_path"`
}

// statusRaw holds the raw outcome of a git.status@v1 execution.
type statusRaw struct {
	validation string
	clean      bool
	branch     string
	head       string
	entries    []statusEntry
	durationMs int64
	ctxErr     error
	runErr     error
}

type statusEntry struct {
	Path     string
	Staging  byte
	Worktree byte
}

func (*statusRaw) IsAdapterRawOutcome() {}

// ClonePayload — see T36.
type ClonePayload struct {
	RepoURL         string   `json:"repo_url"`
	DestinationPath string   `json:"destination_path"`
	Ref             string   `json:"ref,omitempty"`
	Depth           int      `json:"depth,omitempty"`
	AuthMode        AuthMode `json:"auth_mode,omitempty"`
}

// cloneRaw holds the raw outcome of a git.clone@v1 execution.
// AllowsPartial=true (D4.6): when fetchOK=true but checkoutOK=false and
// repoPath is non-empty the normalizer classifies the outcome as "partial".
type cloneRaw struct {
	validation  string
	fetchOK     bool
	checkoutOK  bool
	repoPath    string // resolved destination (populated once PlainCloneContext begins)
	headSHA     string // populated when fetch succeeds and HEAD is readable
	refResolved string // ref short-name, e.g. "main", "v1.0.0"
	durationMs  int64
	ctxErr      error
	runErr      error
}

func (*cloneRaw) IsAdapterRawOutcome() {}

// DiffPayload — see T37.
type DiffPayload struct {
	RepoPath string `json:"repo_path"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

// diffRaw holds the raw outcome of a git.diff@v1 execution.
type diffRaw struct {
	validation string
	patch      []byte
	truncated  bool
	totalBytes int // pre-truncation size for adapter_meta
	durationMs int64
	ctxErr     error
	runErr     error
}

func (*diffRaw) IsAdapterRawOutcome() {}

// CommitPayload — see T38.
type CommitPayload struct {
	RepoPath    string   `json:"repo_path"`
	Message     string   `json:"message"`
	AuthorName  string   `json:"author_name"`
	AuthorEmail string   `json:"author_email"`
	Paths       []string `json:"paths,omitempty"`
	AllowEmpty  bool     `json:"allow_empty,omitempty"`
}

// commitRaw holds the raw outcome of a git.commit@v1 execution.
type commitRaw struct {
	validation string
	commitSHA  string
	branch     string
	durationMs int64
	ctxErr     error
	runErr     error
}

func (*commitRaw) IsAdapterRawOutcome() {}

// Default capability timeouts mirror vo.NewPhase1Capabilities for the
// integration test default.
const (
	defaultStatusTimeout = 10 * time.Second
	defaultCloneTimeout  = 120 * time.Second
	defaultDiffTimeout   = 15 * time.Second
	defaultCommitTimeout = 15 * time.Second
)
