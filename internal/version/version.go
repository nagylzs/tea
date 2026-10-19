// Package version holds build information injected by scripts/build.py via
// -ldflags "-X". Other builds (go install, a plain go build) fall back to the
// module version and commit that the Go toolchain embeds, see fromBuildInfo.
package version

import (
	"fmt"
	"runtime/debug"
	"time"
)

// Version is the output of "git describe --tags --match 'v*' --always --dirty".
var Version = "dev"

var Commit = "unset"

// Branch is empty when the build was made from a detached HEAD, such as the
// tag checkout of a release build; the version names the tag then.
var Branch = "unset"

// Built is the build time in RFC 3339 format, UTC.
var Built = "unset"

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		fromBuildInfo(info)
	}
}

// fromBuildInfo fills in what -ldflags did not set: the module version
// ("v2.1.1" for go install ...@v2.1.1, a pseudo-version for a plain go build)
// and the commit (only known when the build ran in a git checkout).
func fromBuildInfo(info *debug.BuildInfo) {
	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	if Commit == "unset" {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				Commit = s.Value
			}
		}
	}
}

func BuiltTime() *time.Time {
	res, err := time.Parse(time.RFC3339, Built)
	if err != nil {
		return nil
	}
	return &res
}

func LocalVersion() string {
	branch := ""
	if Branch != "" {
		branch = ", branch " + Branch
	}
	return fmt.Sprintf("tea %s (commit %s%s, built %s)", Version, Commit, branch, Built)
}

func PrintVersion() {
	fmt.Println(LocalVersion())
}
