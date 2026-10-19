package version

import (
	"runtime/debug"
	"testing"
)

// set replaces the package variables for one test and restores them after it.
func set(t *testing.T, version, commit, branch, built string) {
	t.Helper()
	old := []string{Version, Commit, Branch, Built}
	Version, Commit, Branch, Built = version, commit, branch, built
	t.Cleanup(func() { Version, Commit, Branch, Built = old[0], old[1], old[2], old[3] })
}

func TestFromBuildInfo(t *testing.T) {
	rev := debug.BuildSetting{Key: "vcs.revision", Value: "ffc56891da44c207eca032f57e14a21d3849a0bd"}
	cases := []struct {
		name          string
		version       string // as injected by -ldflags, or the default
		commit        string
		info          debug.BuildInfo
		wantVersion   string
		wantCommitSet bool
	}{
		{"go install of a release", "dev", "unset",
			debug.BuildInfo{Main: debug.Module{Version: "v2.1.1"}}, "v2.1.1", false},
		{"plain go build in a checkout", "dev", "unset",
			debug.BuildInfo{Main: debug.Module{Version: "v2.1.1-0.20261019075937-ffc56891da44"}, Settings: []debug.BuildSetting{rev}},
			"v2.1.1-0.20261019075937-ffc56891da44", true},
		{"no module version", "dev", "unset",
			debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "dev", false},
		{"built from a file list", "dev", "unset", debug.BuildInfo{}, "dev", false},
		{"-ldflags win", "v2.1.0", "abc",
			debug.BuildInfo{Main: debug.Module{Version: "v2.1.1"}, Settings: []debug.BuildSetting{rev}}, "v2.1.0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set(t, c.version, c.commit, "unset", "unset")
			fromBuildInfo(&c.info)
			if Version != c.wantVersion {
				t.Errorf("Version = %q, want %q", Version, c.wantVersion)
			}
			wantCommit := c.commit
			if c.wantCommitSet {
				wantCommit = rev.Value
			}
			if Commit != wantCommit {
				t.Errorf("Commit = %q, want %q", Commit, wantCommit)
			}
		})
	}
}

func TestLocalVersion(t *testing.T) {
	set(t, "v2.1.1-2-g4d5e6f", "4d5e6f", "main", "2026-10-19T08:00:00Z")
	if got, want := LocalVersion(), "tea v2.1.1-2-g4d5e6f (commit 4d5e6f, branch main, built 2026-10-19T08:00:00Z)"; got != want {
		t.Errorf("LocalVersion() = %q, want %q", got, want)
	}
	// a release build from a tag checkout has no branch
	set(t, "v2.1.1", "1a2b3c", "", "2026-10-19T08:00:00Z")
	if got, want := LocalVersion(), "tea v2.1.1 (commit 1a2b3c, built 2026-10-19T08:00:00Z)"; got != want {
		t.Errorf("LocalVersion() = %q, want %q", got, want)
	}
}
