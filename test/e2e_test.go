// Package e2e runs the built tea binary against small python programs and
// checks its output, exit code and side effects.
//
// Requirements: go, python3 and stdbuf(1) on PATH (Linux only, like tea).
package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var (
	teaBin  string
	testDir string
)

func TestMain(m *testing.M) {
	var err error
	testDir, err = os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "tea-e2e")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	teaBin = filepath.Join(dir, "tea")
	build := exec.Command("go", "build", "-o", teaBin, filepath.Join(testDir, "..", "cmd", "tea", "tea.go"))
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "building tea failed:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	stdout string
	stderr string
	code   int
	took   time.Duration
}

// runTea runs tea with the given arguments and returns its captured streams,
// exit code and running time. It fails the test if tea cannot be started or
// does not finish within 20 seconds.
func runTea(t *testing.T, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, teaBin, args...)
	cmd.Dir = testDir
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	start := time.Now()
	err := cmd.Run()
	took := time.Since(start)
	if ctx.Err() != nil {
		t.Fatalf("tea %q timed out\nstdout:\n%s\nstderr:\n%s", shortArgs(args), so.String(), se.String())
	}
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running tea %q: %v", args, err)
	}
	return result{so.String(), se.String(), code, took}
}

// shortArgs abbreviates long arguments (e.g. big --send-input values) for messages.
func shortArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if len(a) > 40 {
			a = a[:20] + "..." + a[len(a)-10:]
		}
		out[i] = a
	}
	return out
}

// tea builds an argument list: the given tea options, then "--", then the
// python child. The child is always unbuffered (-u).
func tea(opts []string, program ...string) []string {
	return append(append(append([]string{}, opts...), "--", "python3", "-u"), program...)
}

func emit(tokens ...string) []string {
	return append([]string{"emit.py"}, tokens...)
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func sorted(s string) []string {
	l := lines(s)
	sort.Strings(l)
	return l
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func expect(t *testing.T, r result, wantOut string, wantErr string, wantCode int) {
	t.Helper()
	if r.stdout != wantOut {
		t.Errorf("stdout = %q, want %q", r.stdout, wantOut)
	}
	if r.stderr != wantErr {
		t.Errorf("stderr = %q, want %q", r.stderr, wantErr)
	}
	if r.code != wantCode {
		t.Errorf("exit code = %d, want %d", r.code, wantCode)
	}
}

// ---------------------------------------------------------------------------
// Informational options

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{}, {"-h"}, {"--help"}} {
		r := runTea(t, args...)
		if r.code != 0 || !strings.Contains(r.stdout, "tea COMMAND [COMMAND...] -- PROGRAM [ARG...]") {
			t.Errorf("%q: code=%d stdout=%q", args, r.code, r.stdout[:min(60, len(r.stdout))])
		}
	}
}

func TestVersion(t *testing.T) {
	r := runTea(t, "--version")
	// not built via scripts/build.py, so all three fields are "unset"
	expect(t, r, "unset unset unset\n", "", 0)
}

func TestListSignals(t *testing.T) {
	r := runTea(t, "-l")
	if r.code != 0 || !strings.Contains(r.stdout, "SIGTERM = 15\n") || !strings.Contains(r.stdout, "SIGKILL = 9\n") {
		t.Errorf("code=%d stdout=%q", r.code, r.stdout)
	}
}

func TestInvalidArgumentsExitWithError(t *testing.T) {
	r := runTea(t, "-c", "--bogus", "--", "true")
	if r.code != 1 || !strings.Contains(r.stderr, "invalid command: --bogus") || r.stdout != "" {
		t.Errorf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
}

// ---------------------------------------------------------------------------
// Pass-through and exit codes

func TestPassThrough(t *testing.T) {
	r := runTea(t, tea([]string{"-c"}, emit("out:a", "err:b", "out:c", "err:d")...)...)
	expect(t, r, "a\nc\n", "b\nd\n", 0)
}

func TestChildExitCodeIsPropagated(t *testing.T) {
	r := runTea(t, tea([]string{"-c"}, emit("out:a", "exit:3")...)...)
	if r.code != 3 || r.stdout != "a\n" {
		t.Errorf("code=%d stdout=%q", r.code, r.stdout)
	}
	// the exec error text is reported on stderr, never on stdout
	if !strings.Contains(r.stderr, "exit status 3") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestSetExitCode(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "^b$", "--set-exit-code", "42"}, emit("out:a", "out:b", "exit:3")...)...)
	if r.code != 42 {
		t.Errorf("code=%d", r.code)
	}
}

func TestSetExitCodeLastOneWins(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "a", "-e", "10", "-c", "-p", "b", "-e", "20"}, emit("out:b", "out:a")...)...)
	if r.code != 10 {
		t.Errorf("code=%d, want 10", r.code)
	}
}

func TestClearExitCode(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "a", "-e", "10", "-c", "-p", "b", "--clear-exit-code"},
		emit("out:a", "out:b", "exit:5")...)...)
	if r.code != 5 {
		t.Errorf("code=%d, want child's 5", r.code)
	}
}

func TestExitCodeIsSharedBetweenStreams(t *testing.T) {
	// set on stdout chain, cleared on stderr chain; stderr line comes last
	r := runTea(t, tea([]string{"-c", "-p", "a", "-e", "10", "-c", "--std-err", "-p", "b", "--clear-exit-code"},
		emit("out:a", "sleep:0.2", "err:b", "exit:5")...)...)
	if r.code != 5 {
		t.Errorf("code=%d, want 5", r.code)
	}
}

// ---------------------------------------------------------------------------
// Pattern matching

func TestPatterns(t *testing.T) {
	input := emit("out:apple pie", "out:banana", "out:APPLE", "out:cherry")
	cases := []struct {
		name string
		opts []string
		want []string // lines that get the prefix
	}{
		{"single", []string{"-p", "apple"}, []string{"apple pie"}},
		{"regexp flags", []string{"-p", "(?i)apple"}, []string{"apple pie", "APPLE"}},
		{"anchors", []string{"-p", "^a"}, []string{"apple pie"}},
		{"and (default)", []string{"-p", "apple", "-p", "pie"}, []string{"apple pie"}},
		{"and no match", []string{"-p", "apple", "-p", "banana"}, nil},
		{"or", []string{"-p", "apple", "-p", "banana", "--or"}, []string{"apple pie", "banana"}},
		{"no", []string{"-p", "apple", "--no"}, []string{"banana", "APPLE", "cherry"}},
		{"no with two patterns: none may match", []string{"-p", "apple", "-p", "banana", "--no"}, []string{"APPLE", "cherry"}},
		{"or no: at least one does not match", []string{"-p", "apple", "-p", "pie", "--or", "--no"}, []string{"banana", "APPLE", "cherry"}},
		{"patternless matches all", nil, []string{"apple pie", "banana", "APPLE", "cherry"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := append([]string{"-c"}, c.opts...)
			opts = append(opts, "--set-prefix", "> ")
			r := runTea(t, tea(opts, input...)...)
			var got []string
			for _, l := range lines(r.stdout) {
				if strings.HasPrefix(l, "> ") {
					got = append(got, l[2:])
				}
			}
			if !equal(got, c.want) {
				t.Errorf("matched %q, want %q (stdout=%q)", got, c.want, r.stdout)
			}
			if r.code != 0 || r.stderr != "" {
				t.Errorf("code=%d stderr=%q", r.code, r.stderr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stream selection

func TestStreamSelection(t *testing.T) {
	input := emit("out:a", "err:b")
	cases := []struct {
		name         string
		opts         []string
		wantOut, err string
	}{
		{"default: stdout only", []string{"-c", "--set-prefix", "> "}, "> a\n", "b\n"},
		{"--std-err: stderr only", []string{"-c", "--std-err", "--set-prefix", "> "}, "a\n", "> b\n"},
		{"--std-all: both", []string{"-c", "--std-all", "--set-prefix", "> "}, "> a\n", "> b\n"},
		{"-a: both", []string{"-c", "-a", "--set-prefix", "> "}, "> a\n", "> b\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runTea(t, tea(c.opts, input...)...)
			expect(t, r, c.wantOut, c.err, 0)
		})
	}
}

// ---------------------------------------------------------------------------
// Output manipulation

func TestMark(t *testing.T) {
	t.Run("--mark replaces stdout lines", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-m", "."}, emit("out:a", "out:b", "err:c")...)...)
		expect(t, r, "..", "c\n", 0)
	})
	t.Run("--mark-stderr replaces stderr lines", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--std-err", "--mark-stderr", "!"}, emit("out:a", "err:b", "err:c")...)...)
		expect(t, r, "a\n", "!!", 0)
	})
	t.Run("--mark applies to lines read from stdout only", func(t *testing.T) {
		// --mark on a --std-all command: the stderr line keeps its content
		r := runTea(t, tea([]string{"-c", "-a", "-m", "."}, emit("out:a", "err:b")...)...)
		expect(t, r, ".", "b\n", 0)
	})
	t.Run("empty mark suppresses output", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-m", ""}, emit("out:a", "out:b")...)...)
		expect(t, r, "", "", 0)
	})
	t.Run("mark suppresses prefix and suffix", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--set-prefix", "P", "--set-suffix", "S", "-m", "."}, emit("out:a")...)...)
		expect(t, r, ".", "", 0)
	})
	t.Run("last mark wins", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-m", "1", "-c", "-m", "2"}, emit("out:a")...)...)
		expect(t, r, "2", "", 0)
	})
}

func TestPrefixAndSuffix(t *testing.T) {
	t.Run("prefix", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--set-prefix", "[p] "}, emit("out:a", "err:b")...)...)
		expect(t, r, "[p] a\n", "b\n", 0)
	})
	t.Run("suffix replaces the default newline", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--set-suffix", " END\n"}, emit("out:a")...)...)
		expect(t, r, "a END\n", "", 0)
	})
	t.Run("suffix without newline joins lines", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--set-suffix", ","}, emit("out:a", "out:b")...)...)
		expect(t, r, "a,b,", "", 0)
	})
	t.Run("last prefix wins", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--set-prefix", "1", "-c", "--set-prefix", "2"}, emit("out:a")...)...)
		expect(t, r, "2a\n", "", 0)
	})
	t.Run("prefix only on matching lines", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-p", "b", "--set-prefix", "> "}, emit("out:a", "out:b")...)...)
		expect(t, r, "a\n> b\n", "", 0)
	})
}

func TestSendTo(t *testing.T) {
	t.Run("--send-to-stderr", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-p", "b", "--send-to-stderr"}, emit("out:a", "out:b")...)...)
		expect(t, r, "a\n", "b\n", 0)
	})
	t.Run("--send-to-stdout", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--std-err", "--send-to-stdout"}, emit("err:a", "err:b")...)...)
		expect(t, r, "a\nb\n", "", 0)
	})
	t.Run("a later command can undo it", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--send-to-stderr", "-c", "--send-to-stdout"}, emit("out:a")...)...)
		expect(t, r, "a\n", "", 0)
	})
	t.Run("--mark is chosen by input stream: stdout line redirected to stderr", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-m", "X", "--send-to-stderr"}, emit("out:a")...)...)
		expect(t, r, "", "X", 0)
	})
	t.Run("--mark-stderr is chosen by input stream: stderr line redirected to stdout", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--std-err", "--mark-stderr", "X", "--send-to-stdout"}, emit("err:a")...)...)
		expect(t, r, "X", "", 0)
	})
	t.Run("--mark-stderr does not apply to a stdout line sent to stderr", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--mark-stderr", "X", "--send-to-stderr"}, emit("out:a")...)...)
		expect(t, r, "", "a\n", 0)
	})
	t.Run("mark set in an earlier command survives a later redirect", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-m", "X", "-c", "--send-to-stderr"}, emit("out:a")...)...)
		expect(t, r, "", "X", 0)
	})
}

// ---------------------------------------------------------------------------
// Colors (need a pty, otherwise fatih/color disables itself)

func teaPty(opts []string, program ...string) []string {
	// withpty.py runs tea; the tea binary path goes first
	return append([]string{"withpty.py", teaBin}, tea(opts, program...)...)
}

func runUnderPty(t *testing.T, opts []string, program ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", teaPty(opts, program...)...)
	cmd.Dir = testDir
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running withpty.py: %v", err)
	}
	return result{so.String(), se.String(), code, 0}
}

func TestColors(t *testing.T) {
	cases := []struct {
		name string
		opts []string
		want []string // escape sequences that must appear (fatih/color closes attributes with specific reset codes, so only the opening is checked)
	}{
		{"fg red", []string{"--fg-color", "red"}, []string{"\x1b[31mhello"}},
		{"bg blue", []string{"--bg-color", "blue"}, []string{"\x1b[44mhello"}},
		{"hi-green", []string{"--fg-color", "hi-green"}, []string{"\x1b[92mhello"}},
		{"bold", []string{"--bold"}, []string{"\x1b[1mhello"}},
		{"italic", []string{"--italic"}, []string{"\x1b[3mhello"}},
		{"faint", []string{"--faint"}, []string{"\x1b[2mhello"}},
		{"underline", []string{"--underline"}, []string{"\x1b[4mhello"}},
		{"blink", []string{"--blink"}, []string{"\x1b[5mhello"}},
		{"blink-rapid", []string{"--blink-rapid"}, []string{"\x1b[6mhello"}},
		{"combined attributes", []string{"--fg-color", "red", "--bold"}, []string{"\x1b[31;1mhello"}},
		{"prefix and suffix are colored too", []string{"--fg-color", "red", "--set-prefix", "P", "--set-suffix", "S\n"},
			[]string{"\x1b[31mP", "\x1b[31mhello", "\x1b[31mS"}},
		{"mark is colored", []string{"--fg-color", "red", "-m", "."}, []string{"\x1b[31m."}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runUnderPty(t, append([]string{"-c"}, c.opts...), emit("out:hello")...)
			for _, w := range c.want {
				if !strings.Contains(r.stdout, w) {
					t.Errorf("output %q lacks %q", r.stdout, w)
				}
			}
			if r.code != 0 {
				t.Errorf("code=%d", r.code)
			}
		})
	}
	t.Run("stderr lines are colored too", func(t *testing.T) {
		r := runUnderPty(t, []string{"-c", "--std-err", "--fg-color", "red"}, emit("err:hello")...)
		if !strings.Contains(r.stdout, "\x1b[31mhello") {
			t.Errorf("output %q lacks colored stderr line", r.stdout)
		}
	})
	t.Run("last color wins", func(t *testing.T) {
		r := runUnderPty(t, []string{"-c", "--fg-color", "red", "-c", "--fg-color", "green"}, emit("out:hello")...)
		if !strings.Contains(r.stdout, "\x1b[32mhello") || strings.Contains(r.stdout, "\x1b[31m") {
			t.Errorf("output %q", r.stdout)
		}
	})
	t.Run("no color action means no escape sequences", func(t *testing.T) {
		r := runUnderPty(t, []string{"-c", "--set-prefix", "P"}, emit("out:hello")...)
		if strings.Contains(r.stdout, "\x1b[") {
			t.Errorf("output %q contains escape sequences", r.stdout)
		}
	})
	t.Run("color is disabled when stdout is not a terminal", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--fg-color", "red"}, emit("out:hello")...)...)
		expect(t, r, "hello\n", "", 0)
	})
}

// ---------------------------------------------------------------------------
// Command state: --disabled, --disable, --enable, --toggle, --line-*

func TestDisabledCommandNeverRuns(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "--disabled", "--set-prefix", "> "}, emit("out:a")...)...)
	expect(t, r, "a\n", "", 0)
}

func TestDisableSelf(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "once", "--set-prefix", "> ", "--disable", "once"}, emit("out:a", "out:b", "out:c")...)...)
	expect(t, r, "> a\nb\nc\n", "", 0)
}

func TestEnableForwardReference(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "go", "--enable", "x", "-c", "x", "--disabled", "--set-prefix", "> "},
		emit("out:a", "out:go", "out:b")...)...)
	expect(t, r, "a\n> go\n> b\n", "", 0)
}

func TestEnableBackwardReference(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "x", "--disabled", "--set-prefix", "> ", "-c", "-p", "go", "--enable", "x"},
		emit("out:a", "out:go", "out:b")...)...)
	// x is enabled while processing "go", but x comes before the enabler, so it first applies on "b"
	expect(t, r, "a\ngo\n> b\n", "", 0)
}

func TestToggle(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "flip", "--toggle", "x", "-c", "x", "--set-prefix", "> "},
		emit("out:a", "out:flip", "out:b", "out:flip", "out:c")...)...)
	expect(t, r, "> a\nflip\nb\n> flip\n> c\n", "", 0)
}

func TestMultipleDisableInOneCommand(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "off", "--disable", "x", "--disable", "y",
		"-c", "x", "--set-prefix", "x", "-c", "y", "--set-suffix", "y\n"},
		emit("out:a", "out:off", "out:b")...)...)
	// x and y come after the disabling command, so they are already off for the "off" line itself
	expect(t, r, "xay\noff\nb\n", "", 0)
}

func TestLineEnabled(t *testing.T) {
	// b disables itself, but is re-enabled at the start of every line
	r := runTea(t, tea([]string{"-c", "b", "--line-enabled", "--set-prefix", "> ", "--disable", "b"},
		emit("out:a", "out:b", "out:c")...)...)
	expect(t, r, "> a\n> b\n> c\n", "", 0)
}

func TestLineDisabled(t *testing.T) {
	// a enables b for the current line only; b is disabled again on the next line
	r := runTea(t, tea([]string{"-c", "-p", "go", "--enable", "b", "-c", "b", "--line-disabled", "--set-prefix", "> "},
		emit("out:a", "out:go", "out:b")...)...)
	expect(t, r, "a\n> go\nb\n", "", 0)
}

func TestStateIsPerStreamByDefault(t *testing.T) {
	// "once" fires once on stdout and once on stderr
	r := runTea(t, tea([]string{"-c", "once", "-a", "--set-prefix", "> ", "--disable", "once"},
		emit("out:a", "err:b", "out:c", "err:d")...)...)
	expect(t, r, "> a\nc\n", "> b\nd\n", 0)
}

// ---------------------------------------------------------------------------
// Branching

func TestNextLine(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "stop", "--set-prefix", "1", "-n", "-c", "--set-suffix", "2\n"},
		emit("out:a", "out:stop")...)...)
	expect(t, r, "a2\n1stop\n", "", 0)
}

func TestNextLineLeavesLaterCommandsUntouched(t *testing.T) {
	// the third command would disable x, but --next-line prevents it from running
	r := runTea(t, tea([]string{"-c", "-p", "a", "--next-line", "-c", "x", "--set-prefix", "> ", "-c", "-p", "a", "--disable", "x"},
		emit("out:a", "out:b")...)...)
	expect(t, r, "a\n> b\n", "", 0)
}

func TestSkipTo(t *testing.T) {
	r := runTea(t, tea([]string{
		"-c", "-p", "jump", "--skip-to", "last",
		"-c", "--set-prefix", "mid ",
		"-c", "last", "--set-suffix", " end\n"},
		emit("out:a", "out:jump")...)...)
	expect(t, r, "mid a end\njump end\n", "", 0)
}

func TestSkipToDisabledTargetContinuesWithNextEnabled(t *testing.T) {
	r := runTea(t, tea([]string{
		"-c", "--skip-to", "t",
		"-c", "--set-prefix", "skipped ",
		"-c", "t", "--disabled", "--set-prefix", "disabled ",
		"-c", "--set-suffix", " next\n"},
		emit("out:a")...)...)
	expect(t, r, "a next\n", "", 0)
}

// ---------------------------------------------------------------------------
// Stream modes

func TestShareStreams(t *testing.T) {
	// all lines look like stdout, stderr is empty
	r := runTea(t, tea([]string{"--share-streams", "-c", "--set-prefix", "> "},
		emit("out:a", "sleep:0.1", "err:b", "sleep:0.1", "out:c")...)...)
	expect(t, r, "> a\n> b\n> c\n", "", 0)
}

func TestShareStreamsStateIsShared(t *testing.T) {
	r := runTea(t, tea([]string{"--share-streams", "-c", "once", "--set-prefix", "> ", "--disable", "once"},
		emit("out:a", "sleep:0.1", "err:b")...)...)
	expect(t, r, "> a\nb\n", "", 0)
}

func TestShareCommands(t *testing.T) {
	// one chain, lines keep their own streams; "once" fires once in total
	r := runTea(t, tea([]string{"--share-commands", "-c", "once", "-a", "--set-prefix", "> ", "--disable", "once"},
		emit("out:a", "sleep:0.1", "err:b", "sleep:0.1", "out:c")...)...)
	expect(t, r, "> a\nc\n", "b\n", 0)
}

func TestShareCommandsKeepsStreamFilters(t *testing.T) {
	r := runTea(t, tea([]string{"--share-commands", "-c", "--std-err", "--set-prefix", "> "},
		emit("out:a", "err:b")...)...)
	expect(t, r, "a\n", "> b\n", 0)
}

// ---------------------------------------------------------------------------
// Signals, stdin, timed commands

func TestSignalByName(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-s", "SIGUSR1", "-c", "-p", "got SIGUSR1", "--signal", "sigterm"},
		"signals.py")...)
	expect(t, r, "ready\ngot SIGUSR1\ngot SIGTERM\n", "", 0)
}

func TestSignalByNumber(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-s", "12", "-c", "-p", "got SIGUSR2", "-s", "15"},
		"signals.py")...)
	expect(t, r, "ready\ngot SIGUSR2\ngot SIGTERM\n", "", 0)
}

func TestMultipleMatchingCommandsSendMultipleSignals(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-s", "SIGUSR1", "-c", "-p", "^ready$", "-s", "SIGUSR2",
		"-c", "-p", "got SIGUSR2", "-s", "SIGTERM"}, "signals.py")...)
	got := sorted(r.stdout)
	want := []string{"got SIGTERM", "got SIGUSR1", "got SIGUSR2", "ready"}
	if !equal(got, want) || r.code != 0 {
		t.Errorf("stdout=%q code=%d", r.stdout, r.code)
	}
}

func TestSignalKilledChildExitCode(t *testing.T) {
	// SIGKILL is not caught: the child dies by signal, exec reports -1, which becomes 255
	r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-s", "SIGKILL"}, "signals.py")...)
	if r.code != 255 || r.stdout != "ready\n" || !strings.Contains(r.stderr, "signal: killed") {
		t.Errorf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
}

func TestCloseStdin(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "-p", "^ready$", "--close"}, emit("out:ready", "stdin")...)...)
	expect(t, r, "ready\neof\n", "", 0)
}

func TestSendInput(t *testing.T) {
	t.Run("input reaches the child", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-i", "hello\n", "-c", "-p", "^input: hello$", "--close"},
			emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: hello\neof\n", "", 0)
	})
	t.Run("several inputs on one line keep their order", func(t *testing.T) {
		r := runTea(t, tea([]string{
			"-c", "-p", "^ready$", "-i", "a\n",
			"-c", "-p", "^ready$", "--send-input", "b\n",
			"-c", "-p", "^ready$", "-i", "c\n",
			"-c", "-p", "^input: c$", "--close"},
			emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: a\ninput: b\ninput: c\neof\n", "", 0)
	})
	t.Run("input without newline is not a line yet", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-i", "par", "-c", "-p", "^ready$", "-i", "tial\n",
			"-c", "-p", "^input:", "--close"}, emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: partial\neof\n", "", 0)
	})
	t.Run("input and close in the same command: input is written first", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-i", "bye\n", "--close"}, emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: bye\neof\n", "", 0)
	})
	t.Run("input from a later command on the same line is written before the close", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "--close", "-c", "-p", "^ready$", "-i", "late\n"},
			emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: late\neof\n", "", 0)
	})
	t.Run("input after close is dropped with a warning", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "--close", "-c", "-p", "^eof$", "-i", "too late\n"},
			emit("out:ready", "stdin")...)...)
		if r.stdout != "ready\neof\n" || r.code != 0 || !strings.Contains(r.stderr, "already closed") {
			t.Errorf("stdout=%q stderr=%q code=%d", r.stdout, r.stderr, r.code)
		}
	})
	t.Run("input from stderr chain and stdout chain both arrive", func(t *testing.T) {
		r := runTea(t, tea([]string{
			"-c", "-p", "^ready$", "-i", "from-out\n",
			"-c", "--std-err", "-p", "^go$", "-i", "from-err\n",
			"-c", "-p", "^input: from-err$", "--close"},
			emit("out:ready", "sleep:0.2", "err:go", "stdin")...)...)
		expect(t, r, "ready\ninput: from-out\ninput: from-err\neof\n", "go\n", 0)
	})
	t.Run("timed command sends input", func(t *testing.T) {
		r := runTea(t, tea([]string{
			"-c", "idle", "--no-input-for", "500ms", "-i", "wake\n", "--disable", "idle",
			"-c", "-p", "^input: wake$", "--close"},
			emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: wake\neof\n", "", 0)
	})
	t.Run("child that reads stdin only after writing a lot does not deadlock", func(t *testing.T) {
		// Every 5000-byte output line triggers a 20000-byte input while the child
		// is still writing. With a blocking hand-off to the stdin writer, the
		// writer stalls once a pipe buffer (64KB) of input piles up unread, line
		// processing stalls behind it, tea stops draining the child's stdout, and
		// the child (which still has far more than 64KB to write) blocks on its own
		// write: a deadlock. The numbers are chosen so that this happens well
		// before the child gets to read its stdin.
		const n = 40
		tokens := make([]string, 0, n+1)
		for i := 0; i < n; i++ {
			tokens = append(tokens, "long:5000")
		}
		tokens = append(tokens, "stdin")
		big := strings.Repeat("y", 20000) + "\n"
		r := runTea(t, tea([]string{
			"-c", "-p", "^x", "-i", big,
			"-c", "-p", "^input:", "--close",
			"-c", "-m", "."},
			emit(tokens...)...)...)
		// n long lines + n echoed inputs + "eof", each marked with a dot
		expect(t, r, strings.Repeat(".", 2*n+1), "", 0)
	})
}

func TestNoInputForFires(t *testing.T) {
	r := runTea(t, tea([]string{"-c", "idle", "--no-input-for", "500ms", "-e", "42", "-s", "SIGTERM", "--disable", "idle"},
		emit("out:started", "sleep:15")...)...)
	if r.code != 42 || r.stdout != "started\n" {
		t.Errorf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
	if r.took > 5*time.Second {
		t.Errorf("took %v, the idle command should have fired after about one second", r.took)
	}
}

func TestNoInputForIsResetByLines(t *testing.T) {
	// lines keep arriving faster than the duration, so the command never fires
	r := runTea(t, tea([]string{"-c", "--no-input-for", "2s", "-e", "42"},
		emit("out:a", "sleep:0.4", "out:b", "sleep:0.4", "out:c", "sleep:0.4", "out:d")...)...)
	expect(t, r, "a\nb\nc\nd\n", "", 0)
}

func TestNoInputForBetweenLines(t *testing.T) {
	// fires during the pause, then a later line clears the exit code again
	r := runTea(t, tea([]string{"-c", "--no-input-for", "500ms", "-e", "42", "-c", "-p", "b", "--clear-exit-code"},
		emit("out:a", "sleep:2", "out:b")...)...)
	expect(t, r, "a\nb\n", "", 0)
	r = runTea(t, tea([]string{"-c", "--no-input-for", "500ms", "-e", "42"},
		emit("out:a", "sleep:2", "out:b")...)...)
	expect(t, r, "a\nb\n", "", 42)
}

func TestNoInputForRepeatsEveryIdleSecond(t *testing.T) {
	// without a self-disable the command fires repeatedly; count via toggling a marker command
	r := runTea(t, tea([]string{"-c", "--no-input-for", "500ms", "--toggle", "x", "-c", "x", "--disabled", "--set-prefix", "> "},
		emit("out:a", "sleep:1.5", "out:b", "sleep:2.5", "out:c")...)...)
	// the idle timer ticks once per second: one tick in the 1.5s pause (x on -> "> b"),
	// two ticks in the 2.5s pause (x off, then on again -> "> c")
	expect(t, r, "a\n> b\n> c\n", "", 0)
}

// ---------------------------------------------------------------------------
// Global options

func TestPidFile(t *testing.T) {
	pid := filepath.Join(t.TempDir(), "tea.pid")
	// --no-stdbuf so that the pid in the file is python's own pid
	r := runTea(t, tea([]string{"--no-stdbuf", "--pid", pid, "-c"}, emit("sleep:0.5", "pid", "cat:"+pid)...)...)
	l := lines(r.stdout)
	if r.code != 0 || len(l) != 2 || l[0] != l[1] {
		t.Errorf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
	if _, err := os.Stat(pid); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pid file was not removed after exit (stat err = %v)", err)
	}
}

func TestPidFileMustNotExist(t *testing.T) {
	pid := filepath.Join(t.TempDir(), "tea.pid")
	if err := os.WriteFile(pid, []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	r := runTea(t, tea([]string{"--pid", pid, "-c"}, emit("out:a")...)...)
	if r.code != 1 || !strings.Contains(r.stderr, "already exists") || r.stdout != "" {
		t.Errorf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
}

func TestLineBufferSize(t *testing.T) {
	t.Run("lines within the buffer pass", func(t *testing.T) {
		r := runTea(t, tea([]string{"--line-buffer-size", "1024", "-c", "-m", "."}, emit("long:900", "out:after")...)...)
		expect(t, r, "..", "", 0)
	})
	t.Run("a longer line is an error", func(t *testing.T) {
		r := runTea(t, tea([]string{"--line-buffer-size", "1024", "-c", "-m", "."}, emit("long:2000", "out:after")...)...)
		if r.code != 1 || !strings.Contains(r.stderr, "token too long") {
			t.Errorf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
		}
	})
}

func TestStdbufWrapping(t *testing.T) {
	// a glibc stdio program (printf via sh) writing to a pipe would normally
	// block-buffer; with stdbuf line buffering the lines arrive one by one,
	// so the signal sent on the first line stops the program before the second.
	r := runTea(t, "-c", "-p", "first", "-s", "SIGKILL", "--", "sh", "-c", "printf 'first\\n'; sleep 5; printf 'second\\n'")
	if r.stdout != "first\n" || r.took > 4*time.Second {
		t.Errorf("stdout=%q took=%v", r.stdout, r.took)
	}
	t.Run("--no-stdbuf runs the program directly", func(t *testing.T) {
		r := runTea(t, "--no-stdbuf", "-c", "--", "sh", "-c", "echo $0")
		if strings.Contains(r.stdout, "stdbuf") || r.code != 0 {
			t.Errorf("stdout=%q code=%d", r.stdout, r.code)
		}
	})
}
