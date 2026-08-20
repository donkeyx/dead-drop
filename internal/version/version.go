// Package version is the process release string.
// Docker/release sets it with -ldflags. Untagged builds stay "dev".
package version

import (
	"runtime/debug"
	"strings"
)

// Version is set at link time. Do not put a v prefix in the ldflag.
var Version = "dev"

// String is the footer/CLI form: "0.1.6" or "dev".
func String() string {
	v := strings.TrimSpace(Version)
	if v != "" && v != "dev" {
		return strings.TrimPrefix(v, "v")
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if mv := bi.Main.Version; mv != "" && mv != "(devel)" {
			return strings.TrimPrefix(mv, "v")
		}
	}
	return "dev"
}
