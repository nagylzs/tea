// Package version holds build information injected by scripts/build.py via
// -ldflags "-X". A plain "go build" leaves the defaults.
package version

import (
	"fmt"
	"time"
)

// Version is the output of "git describe --tags --match 'v*' --always --dirty".
var Version = "dev"

var Commit = "unset"

var Branch = "unset"

// Built is the build time in RFC 3339 format, UTC.
var Built = "unset"

func BuiltTime() *time.Time {
	res, err := time.Parse(time.RFC3339, Built)
	if err != nil {
		return nil
	}
	return &res
}

func LocalVersion() string {
	return fmt.Sprintf("tea %s (commit %s, branch %s, built %s)", Version, Commit, Branch, Built)
}

func PrintVersion() {
	fmt.Println(LocalVersion())
}
