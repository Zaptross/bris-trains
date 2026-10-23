// Package version reports the version of the trains binary.
//
// Release builds set Version with
//
//	go build -ldflags "-X trains/internal/version.Version=1.4.2" ./cmd/trains
package version

import "runtime/debug"

// Version is the release version, without a "v" prefix. Unset in local builds.
var Version = ""

// String returns the version, or for a local build "dev" plus the VCS
// revision Go embeds, e.g. "dev+a1b2c3d" or "dev+a1b2c3d-dirty".
func String() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "-dirty"
	}
	return "dev+" + rev
}
