// Package filesystem implements the Phase 1 filesystem adapter — 2
// capabilities (filesystem.read_file@v1, filesystem.write_file@v1) per
// spec §8.4. Reads are bounded by max_bytes with truncate flag; writes
// are atomic via tmp+fsync+rename (D8.4 — never partial). Path safety
// enforced via AllowedFilesystemRoots allowlist + symlink resolution.
//
// External contract: AdapterID = "filesystem".
package filesystem

import "time"

// Config bundles the bootstrap-loaded safety settings.
type Config struct {
	// AllowedFilesystemRoots is the list of absolute roots within which
	// any read/write path must resolve (after symlink expansion). Empty
	// list rejects all paths.
	AllowedFilesystemRoots []string

	// InlineStreamLimit caps the read_file content stream when building
	// entities.StreamRef.
	InlineStreamLimit int
}

// ReadFilePayload — wire payload for filesystem.read_file@v1.
type ReadFilePayload struct {
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"` // default 1 MiB
}

type readRaw struct {
	validation string
	data       []byte
	totalBytes int64
	truncated  bool
	durationMs int64
	ctxErr     error
	runErr     error
}

func (*readRaw) IsAdapterRawOutcome() {}

// WriteFilePayload — wire payload for filesystem.write_file@v1.
type WriteFilePayload struct {
	Path       string `json:"path"`
	Data       []byte `json:"data"`                  // base64 on wire
	Overwrite  bool   `json:"overwrite,omitempty"`   // default false
	CreateDirs bool   `json:"create_dirs,omitempty"` // default false
	Mode       uint32 `json:"mode,omitempty"`        // default 0o600
}

type writeRaw struct {
	validation   string
	resolvedPath string
	bytesWritten int64
	checksumHex  string // sha256 hex
	mode         uint32
	durationMs   int64
	ctxErr       error
	runErr       error
}

func (*writeRaw) IsAdapterRawOutcome() {}

const (
	defaultReadTimeout  = 5 * time.Second
	defaultWriteTimeout = 10 * time.Second
)
