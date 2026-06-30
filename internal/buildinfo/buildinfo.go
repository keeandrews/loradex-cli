// Package buildinfo holds build-time metadata injected via -ldflags.
package buildinfo

import "runtime"

// These are set at build time with:
//
//	-ldflags "-X .../buildinfo.Version=v1.2.3 -X .../buildinfo.Commit=abc -X .../buildinfo.Date=..."
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// GoVersion returns the Go toolchain used to build the binary.
func GoVersion() string { return runtime.Version() }

// Platform returns "<goos>/<goarch>".
func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }
