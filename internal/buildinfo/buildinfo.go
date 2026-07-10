// Package buildinfo exposes the version metadata stamped into the binary at
// build time via -ldflags. The defaults below are used for a plain local
// `go build`/`go run`, where no linker flags are supplied; the Dockerfile and
// the release workflow override them with the real release values.
package buildinfo

import "runtime/debug"

// These variables are overridden at build time, for example:
//
//	go build -ldflags "-X github.com/kenlasko/adguard-logcentral/internal/buildinfo.Version=1.2.3"
//
// See the Dockerfile and .github/workflows/release.yml for the release build.
var (
	// Version is the semantic version of the release, without a leading "v".
	Version = "dev"
	// Commit is the git SHA the binary was built from.
	Commit = "none"
	// Date is the RFC3339 UTC build timestamp.
	Date = "unknown"
)

// Info is an immutable snapshot of the build metadata.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Get returns the build metadata. When Version was not stamped in (a plain
// local build), it falls back to the module version recorded by the Go
// toolchain when one is available, so `go install ...@v1.2.3` still reports a
// real version instead of "dev".
func Get() Info {
	version := Version
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			if v := bi.Main.Version; v != "" && v != "(devel)" {
				version = v
			}
		}
	}
	return Info{Version: version, Commit: Commit, Date: Date}
}

// String renders the build metadata on a single line, for example
// "1.2.3 (commit abc1234, built 2026-07-10T12:00:00Z)".
func (i Info) String() string {
	return i.Version + " (commit " + i.Commit + ", built " + i.Date + ")"
}
