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
	"io"
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

// stdinSpec says what tea's own stdin is during a test.
type stdinSpec struct {
	null     bool   // /dev/null: immediate end of file
	data     string // piped data, followed by end of file unless keepOpen
	keepOpen bool   // after data, keep the pipe open (like a terminal after one line)
	// neither null nor data: a pipe that is never written, like an idle terminal
}

var (
	stdinOpen = stdinSpec{}
	stdinNull = stdinSpec{null: true}
)

func stdinData(s string) stdinSpec     { return stdinSpec{data: s} }
func stdinDataOpen(s string) stdinSpec { return stdinSpec{data: s, keepOpen: true} }

// runTea runs tea with an idle (open, never written) stdin. See runTeaStdin.
func runTea(t *testing.T, args ...string) result {
	t.Helper()
	return runTeaStdin(t, stdinOpen, args...)
}

// runTeaStdin runs tea with the given stdin and arguments and returns its
// captured streams, exit code and running time. It fails the test if tea
// cannot be started or does not finish within 20 seconds.
func runTeaStdin(t *testing.T, in stdinSpec, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, teaBin, args...)
	cmd.Dir = testDir
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	if !in.null {
		// an *os.File is handed to tea directly, so cmd.Wait does not wait for
		// a copying goroutine and the pipe can stay open as long as we like
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdin = r
		defer r.Close()
		defer w.Close()
		if in.data != "" {
			go func() {
				_, _ = io.WriteString(w, in.data)
				if !in.keepOpen {
					w.Close()
				}
			}()
		}
	}
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

func TestImplicitFirstCommand(t *testing.T) {
	r := runTea(t, tea([]string{"-p", "b", "--set-prefix", "> ", "-c", "-p", "a", "--set-suffix", "!\n"}, emit("out:a", "out:b")...)...)
	expect(t, r, "a!\n> b\n", "", 0)
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
	t.Run("--send-input-file writes the file contents", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "in.txt")
		if err := os.WriteFile(file, []byte("one\ntwo\n"), 0644); err != nil {
			t.Fatal(err)
		}
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-f", file, "-c", "-p", "^input: two$", "--close"},
			emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: one\ninput: two\neof\n", "", 0)
	})
	t.Run("--send-input is written before --send-input-file of the same command", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "in.txt")
		if err := os.WriteFile(file, []byte("file\n"), 0644); err != nil {
			t.Fatal(err)
		}
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "--send-input", "from ", "--send-input-file", file, "--close"},
			emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: from file\neof\n", "", 0)
	})
	t.Run("file without trailing newline is not a line yet", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "in.txt")
		if err := os.WriteFile(file, []byte("no newline"), 0644); err != nil {
			t.Fatal(err)
		}
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-f", file, "-c", "-p", "^ready$", "-i", "!\n", "--close"},
			emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: no newline!\neof\n", "", 0)
	})
	t.Run("file may be created after tea starts", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "later.txt")
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-f", file, "-c", "-p", "^input:", "--close"},
			emit("write:"+file+"=made by child", "out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: made by child\neof\n", "", 0)
	})
	t.Run("missing file is a fatal error", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "missing.txt")
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "-f", file}, emit("out:ready", "sleep:5")...)...)
		if r.code != 1 || !strings.Contains(r.stderr, "--send-input-file") || !strings.Contains(r.stderr, "missing.txt") {
			t.Errorf("code=%d stderr=%q", r.code, r.stderr)
		}
		if r.took > 4*time.Second {
			t.Errorf("took %v, tea should have exited immediately", r.took)
		}
	})
	t.Run("timed command sends a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "in.txt")
		if err := os.WriteFile(file, []byte("wake\n"), 0644); err != nil {
			t.Fatal(err)
		}
		r := runTea(t, tea([]string{
			"-c", "idle", "--no-input-for", "500ms", "-f", file, "--disable", "idle",
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
// --timeout

func TestTimeout(t *testing.T) {
	t.Run("deadline fires", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "--timeout", "500ms", "-e", "1", "-s", "SIGTERM"},
			emit("out:started", "sleep:15")...)...)
		if r.code != 1 || r.stdout != "started\n" {
			t.Errorf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
		}
		if r.took > 5*time.Second {
			t.Errorf("took %v, the deadline should have fired after about one second", r.took)
		}
	})
	t.Run("deadline not reached when the child finishes first", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-t", "5s", "-e", "1"}, emit("out:a")...)...)
		expect(t, r, "a\n", "", 0)
		if r.took > 3*time.Second {
			t.Errorf("took %v, tea should exit with the child", r.took)
		}
	})
	t.Run("readiness wait with deadline: ready first", func(t *testing.T) {
		r := runTea(t, tea([]string{
			"-c", "-p", "ready", "-e", "0", "-s", "SIGTERM",
			"-c", "-t", "5s", "-e", "1", "-s", "SIGTERM"},
			emit("out:starting", "sleep:0.3", "out:ready", "sleep:15")...)...)
		if r.code != 0 || r.stdout != "starting\nready\n" {
			t.Errorf("code=%d stdout=%q", r.code, r.stdout)
		}
	})
	t.Run("readiness wait with deadline: deadline first", func(t *testing.T) {
		r := runTea(t, tea([]string{
			"-c", "-p", "ready", "-e", "0", "-s", "SIGTERM",
			"-c", "-t", "500ms", "-e", "1", "-s", "SIGTERM"},
			emit("out:starting", "sleep:15", "out:ready")...)...)
		if r.code != 1 || r.stdout != "starting\n" {
			t.Errorf("code=%d stdout=%q", r.code, r.stdout)
		}
	})
	t.Run("deadline counts from when the command was enabled", func(t *testing.T) {
		// step1 appears at 1.5s; the 500ms deadline must not fire before that
		r := runTea(t, tea([]string{
			"-c", "-p", "^step1$", "--enable", "t",
			"-c", "t", "--disabled", "--timeout", "500ms", "-e", "7", "-s", "SIGTERM"},
			emit("out:a", "sleep:1.5", "out:step1", "sleep:15")...)...)
		if r.code != 7 || r.stdout != "a\nstep1\n" {
			t.Errorf("code=%d stdout=%q", r.code, r.stdout)
		}
		if r.took < 2*time.Second || r.took > 6*time.Second {
			t.Errorf("took %v, expected roughly 1.5s + up to 2 ticks", r.took)
		}
	})
	t.Run("fires once, not on every tick", func(t *testing.T) {
		// toggling x on every tick would flip the prefix on and off
		r := runTea(t, tea([]string{
			"-c", "-t", "500ms", "--toggle", "x",
			"-c", "x", "--disabled", "--set-prefix", "> "},
			emit("out:a", "sleep:1.5", "out:b", "sleep:1", "out:c", "sleep:1", "out:d")...)...)
		expect(t, r, "a\n> b\n> c\n> d\n", "", 0)
	})
	t.Run("re-enabling restarts the clock", func(t *testing.T) {
		// t fires (x on, t off); "a" re-enables t; t fires again (x off)
		r := runTea(t, tea([]string{
			"-c", "t", "-t", "500ms", "--toggle", "x", "--disable", "t",
			"-c", "-p", "^a$", "--enable", "t",
			"-c", "x", "--disabled", "--set-prefix", "> "},
			emit("sleep:1.5", "out:a", "sleep:1.5", "out:b")...)...)
		expect(t, r, "> a\nb\n", "", 0)
	})
	t.Run("enabled from the stderr chain", func(t *testing.T) {
		r := runTea(t, tea([]string{
			"-c", "--std-err", "-p", "^go$", "--enable", "t",
			"-c", "t", "--disabled", "-t", "500ms", "-e", "9", "-s", "SIGTERM"},
			emit("err:go", "sleep:15")...)...)
		if r.code != 9 || r.stderr != "go\n" {
			t.Errorf("code=%d stderr=%q", r.code, r.stderr)
		}
	})
	t.Run("a timed command's --disable applies to both chains", func(t *testing.T) {
		r := runTea(t, tea([]string{
			"-c", "-t", "500ms", "--disable", "x",
			"-c", "x", "-a", "--set-prefix", "> "},
			emit("out:a", "err:b", "sleep:1.5", "out:c", "err:d")...)...)
		expect(t, r, "> a\nc\n", "> b\nd\n", 0)
	})
	t.Run("timed command sends input once, not once per chain", func(t *testing.T) {
		// close the child's stdin only after two quiet seconds, so every queued
		// input is echoed and can be counted
		r := runTea(t, tea([]string{
			"-c", "i", "-t", "500ms", "-i", "x\n", "--disable", "i",
			"-c", "-p", "^input: x$", "--enable", "q",
			"-c", "q", "--disabled", "--no-input-for", "2s", "--close"},
			emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: x\neof\n", "", 0)
	})
}

func TestNoInputForCountsBothStreams(t *testing.T) {
	// stdout is quiet for 1s at a time, but stderr fills the gaps: never idle for 800ms
	r := runTea(t, tea([]string{"-c", "--no-input-for", "800ms", "-e", "5"},
		emit("out:a", "sleep:0.5", "err:b", "sleep:0.5", "out:c", "sleep:0.5", "err:d", "sleep:0.5", "out:e")...)...)
	expect(t, r, "a\nc\ne\n", "b\nd\n", 0)
}

func TestNoInputForFiresOnceNotPerChain(t *testing.T) {
	r := runTea(t, tea([]string{
		"-c", "i", "--no-input-for", "500ms", "-i", "x\n", "--disable", "i",
		"-c", "-p", "^input: x$", "--enable", "q",
		"-c", "q", "--disabled", "--no-input-for", "2s", "--close"},
		emit("out:ready", "stdin")...)...)
	expect(t, r, "ready\ninput: x\neof\n", "", 0)
}

// ---------------------------------------------------------------------------
// Forwarding tea's own stdin

func TestStdinForwarding(t *testing.T) {
	t.Run("lines and end of file are forwarded", func(t *testing.T) {
		r := runTeaStdin(t, stdinData("a\nb\n"), tea([]string{"-c"}, emit("stdin")...)...)
		expect(t, r, "input: a\ninput: b\neof\n", "", 0)
	})
	t.Run("partial last line is forwarded as is", func(t *testing.T) {
		r := runTeaStdin(t, stdinData("abc"), tea([]string{"-c"}, emit("stdin")...)...)
		expect(t, r, "input: abc\neof\n", "", 0)
	})
	t.Run("large input arrives intact", func(t *testing.T) {
		big := strings.Repeat("0123456789abcdef\n", 65536) // 1 MiB
		r := runTeaStdin(t, stdinData(big), tea([]string{"-c"}, emit("count")...)...)
		expect(t, r, fmt.Sprintf("bytes: %d\n", len(big)), "", 0)
	})
	t.Run("binary data passes through unchanged", func(t *testing.T) {
		bin := make([]byte, 256)
		for i := range bin {
			bin[i] = byte(i)
		}
		r := runTeaStdin(t, stdinData(string(bin)), tea([]string{"-c"}, emit("count")...)...)
		expect(t, r, "bytes: 256\n", "", 0)
	})
	t.Run("/dev/null closes the child's stdin immediately", func(t *testing.T) {
		r := runTeaStdin(t, stdinNull, tea([]string{"-c"}, emit("stdin")...)...)
		expect(t, r, "eof\n", "", 0)
	})
	t.Run("an idle stdin keeps the child's stdin open until --close", func(t *testing.T) {
		r := runTea(t, tea([]string{"-c", "-p", "^ready$", "--close"}, emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\neof\n", "", 0)
	})
	t.Run("forwarded input and --send-input share one ordered stream", func(t *testing.T) {
		// "one" arrives on tea's stdin (which then stays open, like a terminal);
		// when the child echoes it, a command sends "two", then --close ends it.
		r := runTeaStdin(t, stdinDataOpen("one\n"), tea([]string{
			"-c", "-p", "^input: one$", "-i", "two\n",
			"-c", "-p", "^input: two$", "--close"}, emit("stdin")...)...)
		expect(t, r, "input: one\ninput: two\neof\n", "", 0)
	})
	t.Run("forwarded end of file drops a later --send-input with a warning", func(t *testing.T) {
		r := runTeaStdin(t, stdinNull, tea([]string{"-c", "-p", "^eof$", "-i", "late\n"}, emit("stdin", "sleep:0.3")...)...)
		if r.stdout != "eof\n" || r.code != 0 || !strings.Contains(r.stderr, "already closed") {
			t.Errorf("stdout=%q stderr=%q code=%d", r.stdout, r.stderr, r.code)
		}
	})
	t.Run("piped input to a child that never reads it does not break tea", func(t *testing.T) {
		big := strings.Repeat("x", 1<<20)
		r := runTeaStdin(t, stdinData(big), tea([]string{"-c"}, emit("out:a", "out:b")...)...)
		expect(t, r, "a\nb\n", "", 0)
	})
}

func TestNoStdin(t *testing.T) {
	t.Run("piped input is not forwarded", func(t *testing.T) {
		r := runTeaStdin(t, stdinData("a\n"), tea([]string{"--no-stdin", "-c", "-p", "^ready$", "--close"},
			emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\neof\n", "", 0)
	})
	t.Run("/dev/null does not close the child's stdin, --send-input still works", func(t *testing.T) {
		r := runTeaStdin(t, stdinNull, tea([]string{"--no-stdin", "-c", "-p", "^ready$", "-i", "hello\n",
			"-c", "-p", "^input: hello$", "--close"}, emit("out:ready", "stdin")...)...)
		expect(t, r, "ready\ninput: hello\neof\n", "", 0)
	})
	t.Run("child waiting for stdin ends when it exits on its own", func(t *testing.T) {
		r := runTeaStdin(t, stdinNull, tea([]string{"--no-stdin", "-c"}, emit("out:a", "exit:4")...)...)
		if r.stdout != "a\n" || r.code != 4 {
			t.Errorf("stdout=%q code=%d", r.stdout, r.code)
		}
	})
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
