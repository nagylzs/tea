package opts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// parse resets the package-global parser state and parses the given argument
// list as if it were the command line (without the program name).
func parse(t *testing.T, args ...string) (Type, error) {
	t.Helper()
	Opts = defaultOpts()
	argIdx = 0
	cmdIdx = -1
	os.Args = append([]string{"tea"}, args...)
	return ParseArgs()
}

// mustParse parses and fails the test on error.
func mustParse(t *testing.T, args ...string) Type {
	t.Helper()
	o, err := parse(t, args...)
	if err != nil {
		t.Fatalf("unexpected parse error for %q: %v", args, err)
	}
	return o
}

// tail is the program part appended to most test invocations.
var tail = []string{"--no-stdbuf", "--", "true"}

func withTail(args ...string) []string {
	return append(append([]string{}, args...), tail...)
}

func TestNoArgsMeansHelp(t *testing.T) {
	o := mustParse(t)
	if !o.Help {
		t.Errorf("expected Help to be set")
	}
}

func TestInformationalOptions(t *testing.T) {
	cases := []struct {
		args  []string
		check func(Type) bool
	}{
		{[]string{"-h"}, func(o Type) bool { return o.Help }},
		{[]string{"--help"}, func(o Type) bool { return o.Help }},
		{[]string{"--version"}, func(o Type) bool { return o.ShowVersion }},
		{[]string{"-l"}, func(o Type) bool { return o.ListSignals }},
		{[]string{"--list-signals"}, func(o Type) bool { return o.ListSignals }},
		// they short-circuit: nothing after them is parsed or validated
		{[]string{"-h", "--this-is-not-an-option"}, func(o Type) bool { return o.Help }},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			o := mustParse(t, c.args...)
			if !c.check(o) {
				t.Errorf("flag not set for %q: %+v", c.args, o)
			}
		})
	}
}

func TestProgramAndArgs(t *testing.T) {
	t.Run("stdbuf wrapping by default", func(t *testing.T) {
		o := mustParse(t, "-c", "--", "true", "a", "b")
		if filepath.Base(o.Program) != "stdbuf" {
			t.Errorf("Program = %q, want stdbuf", o.Program)
		}
		want := []string{"-oL", "-eL"}
		if len(o.ProgramArgs) != 5 || o.ProgramArgs[0] != want[0] || o.ProgramArgs[1] != want[1] ||
			filepath.Base(o.ProgramArgs[2]) != "true" || o.ProgramArgs[3] != "a" || o.ProgramArgs[4] != "b" {
			t.Errorf("ProgramArgs = %q", o.ProgramArgs)
		}
	})
	t.Run("--no-stdbuf runs the program directly", func(t *testing.T) {
		o := mustParse(t, "--no-stdbuf", "-c", "--", "true", "x")
		if !o.NoStdBuf || filepath.Base(o.Program) != "true" {
			t.Errorf("Program = %q NoStdBuf = %v", o.Program, o.NoStdBuf)
		}
		if len(o.ProgramArgs) != 1 || o.ProgramArgs[0] != "x" {
			t.Errorf("ProgramArgs = %q", o.ProgramArgs)
		}
	})
	t.Run("missing stdbuf falls back to a direct start", func(t *testing.T) {
		tru, err := exec.LookPath("true")
		if err != nil {
			t.Skip("no true(1)")
		}
		t.Setenv("PATH", t.TempDir()) // nothing on PATH, so stdbuf cannot be found
		o := mustParse(t, "-c", "--", tru, "x")
		if !o.StdBufMissing || o.Program != tru || len(o.ProgramArgs) != 1 || o.ProgramArgs[0] != "x" {
			t.Errorf("StdBufMissing=%v Program=%q ProgramArgs=%q", o.StdBufMissing, o.Program, o.ProgramArgs)
		}
		o = mustParse(t, "--no-stdbuf", "-c", "--", tru)
		if o.StdBufMissing {
			t.Errorf("StdBufMissing set although --no-stdbuf was given")
		}
	})
	t.Run("--no-stdbuf without args gives an empty arg list", func(t *testing.T) {
		o := mustParse(t, "--no-stdbuf", "-c", "--", "true")
		if o.ProgramArgs == nil || len(o.ProgramArgs) != 0 {
			t.Errorf("ProgramArgs = %#v", o.ProgramArgs)
		}
	})
	t.Run("program is resolved via PATH", func(t *testing.T) {
		o := mustParse(t, "--no-stdbuf", "-c", "--", "true")
		if !filepath.IsAbs(o.Program) {
			t.Errorf("Program = %q, want absolute path", o.Program)
		}
	})
}

func TestGlobalOptions(t *testing.T) {
	t.Run("--line-buffer-size", func(t *testing.T) {
		o := mustParse(t, withTail("--line-buffer-size", "4096", "-c")...)
		if o.LineBufferSize != 4096 {
			t.Errorf("LineBufferSize = %d", o.LineBufferSize)
		}
	})
	t.Run("default line buffer size", func(t *testing.T) {
		o := mustParse(t, withTail("-c")...)
		if o.LineBufferSize != 65535 {
			t.Errorf("LineBufferSize = %d", o.LineBufferSize)
		}
	})
	t.Run("--no-stdin", func(t *testing.T) {
		o := mustParse(t, withTail("--no-stdin", "-c")...)
		if !o.NoStdIn {
			t.Errorf("NoStdIn not set")
		}
		o = mustParse(t, withTail("-c")...)
		if o.NoStdIn {
			t.Errorf("NoStdIn set by default")
		}
	})
	t.Run("--stop-signal", func(t *testing.T) {
		o := mustParse(t, withTail("-c")...)
		if o.StopSignal != syscall.SIGTERM {
			t.Errorf("default StopSignal = %v", o.StopSignal)
		}
		o = mustParse(t, withTail("--stop-signal", "SIGINT", "-c")...)
		if o.StopSignal != syscall.SIGINT {
			t.Errorf("StopSignal = %v", o.StopSignal)
		}
	})
	t.Run("--share-commands", func(t *testing.T) {
		o := mustParse(t, withTail("--share-commands", "-c")...)
		if !o.ShareCommands || o.ShareStreams {
			t.Errorf("ShareCommands=%v ShareStreams=%v", o.ShareCommands, o.ShareStreams)
		}
	})
	t.Run("--share-streams", func(t *testing.T) {
		o := mustParse(t, withTail("--share-streams", "-c")...)
		if o.ShareCommands || !o.ShareStreams {
			t.Errorf("ShareCommands=%v ShareStreams=%v", o.ShareCommands, o.ShareStreams)
		}
	})
	t.Run("--pid", func(t *testing.T) {
		pid := filepath.Join(t.TempDir(), "tea.pid")
		o := mustParse(t, withTail("--pid", pid, "-c")...)
		if o.PidFile != pid {
			t.Errorf("PidFile = %q", o.PidFile)
		}
	})
}

func TestImplicitFirstCommand(t *testing.T) {
	t.Run("a command level option starts the first command", func(t *testing.T) {
		o := mustParse(t, withTail("-p", "x", "-s", "SIGINT")...)
		if len(o.Commands) != 1 || o.Commands[0].Name != "" || len(o.Commands[0].Conditions.RawPatterns) != 1 {
			t.Errorf("commands = %+v", o.Commands)
		}
	})
	t.Run("-c after an implicit first command starts the second", func(t *testing.T) {
		o := mustParse(t, withTail("-p", "x", "-c", "second", "-p", "y")...)
		if len(o.Commands) != 2 || o.Commands[1].Name != "second" || o.CmdIdx["second"] != 1 {
			t.Errorf("commands = %+v idx = %v", o.Commands, o.CmdIdx)
		}
	})
	t.Run("global options may follow the implicit command", func(t *testing.T) {
		o := mustParse(t, "-p", "x", "--no-stdbuf", "--", "true")
		if len(o.Commands) != 1 || !o.NoStdBuf {
			t.Errorf("commands = %+v NoStdBuf = %v", o.Commands, o.NoStdBuf)
		}
	})
	t.Run("no command level option at all is still an error", func(t *testing.T) {
		_, err := parse(t, tail...)
		if err == nil || !strings.Contains(err.Error(), "at least one command") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestCommandNames(t *testing.T) {
	t.Run("named and unnamed commands, CmdIdx", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "first", "-c", "--command", "third")...)
		if len(o.Commands) != 3 {
			t.Fatalf("got %d commands", len(o.Commands))
		}
		if o.Commands[0].Name != "first" || o.Commands[1].Name != "" || o.Commands[2].Name != "third" {
			t.Errorf("names = %q %q %q", o.Commands[0].Name, o.Commands[1].Name, o.Commands[2].Name)
		}
		if o.CmdIdx["first"] != 0 || o.CmdIdx["third"] != 2 || len(o.CmdIdx) != 2 {
			t.Errorf("CmdIdx = %v", o.CmdIdx)
		}
	})
	t.Run("a name is optional before an option", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "-p", "x")...)
		if o.Commands[0].Name != "" || len(o.Commands[0].Conditions.RawPatterns) != 1 {
			t.Errorf("command = %+v", o.Commands[0])
		}
	})
	t.Run("a name is optional before --", func(t *testing.T) {
		o := mustParse(t, "--no-stdbuf", "-c", "--", "true")
		if o.Commands[0].Name != "" {
			t.Errorf("name = %q", o.Commands[0].Name)
		}
	})
}

func TestConditions(t *testing.T) {
	t.Run("patterns are compiled and ANDed by default", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "-p", "foo", "--pattern", "(?i)bar")...)
		c := o.Commands[0].Conditions
		if len(c.RawPatterns) != 2 || len(c.CompiledPatterns) != 2 {
			t.Fatalf("patterns = %v / %v", c.RawPatterns, c.CompiledPatterns)
		}
		if !c.CompiledPatterns[1].MatchString("BAR") {
			t.Errorf("regexp flags were not honoured")
		}
		if c.Or || c.No {
			t.Errorf("Or/No set by default")
		}
	})
	t.Run("--or and --no", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "-p", "a", "--or", "--no")...)
		c := o.Commands[0].Conditions
		if !c.Or || !c.No {
			t.Errorf("Or=%v No=%v", c.Or, c.No)
		}
	})
	t.Run("stream selection", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "-c", "--std-err", "-c", "-a", "-c", "--std-all")...)
		want := []struct{ out, err bool }{{true, false}, {false, true}, {true, true}, {true, true}}
		for i, w := range want {
			c := o.Commands[i].Conditions
			if c.StdOut != w.out || c.StdErr != w.err {
				t.Errorf("command %d: StdOut=%v StdErr=%v, want %v/%v", i, c.StdOut, c.StdErr, w.out, w.err)
			}
		}
	})
	t.Run("--timeout", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "-t", "2s", "-c", "--timeout", "1m")...)
		for i, want := range []time.Duration{2 * time.Second, time.Minute} {
			d := o.Commands[i].Conditions.Timeout
			if d == nil || *d != want {
				t.Errorf("command %d: Timeout = %v, want %v", i, d, want)
			}
			if !o.Commands[i].IsTimed() {
				t.Errorf("command %d: IsTimed() = false", i)
			}
		}
	})
	t.Run("line commands are not timed", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "-p", "x")...)
		if o.Commands[0].IsTimed() {
			t.Errorf("IsTimed() = true")
		}
	})
	t.Run("--no-input-for", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "--no-input-for", "1500ms")...)
		d := o.Commands[0].Conditions.NoInputFor
		if d == nil || *d != 1500*time.Millisecond {
			t.Errorf("NoInputFor = %v", d)
		}
	})
}

func TestActions(t *testing.T) {
	o := mustParse(t, withTail(
		"-c", "a",
		"--mark", "M", "--mark-stderr", "E", "--set-prefix", "P", "--set-suffix", "S",
		"--send-to-stderr", "--next-line", "--skip-to", "b",
		"--disable", "b", "--disable", "c", "--enable", "a", "--toggle", "d",
		"--signal", "SIGUSR1", "--send-input", "in\n", "--send-input-file", "/nonexistent",
		"--close", "--set-exit-code", "7",
		"-c", "b", "--clear-exit-code", "--send-to-stdout", "-m", "x", "-n", "-s", "9", "-i", "y", "-f", "z",
		"-c", "c", "-e", "0", "-c", "d",
	)...)
	a := o.Commands[0].Actions
	checks := []struct {
		name string
		ok   bool
	}{
		{"mark", a.MarkStdOut != nil && *a.MarkStdOut == "M"},
		{"mark-stderr", a.MarkStdErr != nil && *a.MarkStdErr == "E"},
		{"set-prefix", a.SetPrefix != nil && *a.SetPrefix == "P"},
		{"set-suffix", a.SetSuffix != nil && *a.SetSuffix == "S"},
		{"send-to-stderr", a.SendToStdErr && !a.SendToStdOut},
		{"next-line", a.NextLine},
		{"skip-to", a.SkipTo != nil && *a.SkipTo == "b"},
		{"disable", len(a.Disable) == 2 && a.Disable[0] == "b" && a.Disable[1] == "c"},
		{"enable", len(a.Enable) == 1 && a.Enable[0] == "a"},
		{"toggle", len(a.Toggle) == 1 && a.Toggle[0] == "d"},
		{"signal", a.Signal != nil && *a.Signal == syscall.SIGUSR1},
		{"send-input", a.Input != nil && *a.Input == "in\n"},
		{"send-input-file", a.InputFile != nil && *a.InputFile == "/nonexistent"},
		{"close", a.CloseStdIn},
		{"set-exit-code", a.SetExitCode != nil && *a.SetExitCode == 7},
		{"clear-exit-code unset", !a.ClearExitCode},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("command a: %s not parsed as expected: %+v", c.name, a)
		}
	}
	b := o.Commands[1].Actions
	checks = []struct {
		name string
		ok   bool
	}{
		{"clear-exit-code", b.ClearExitCode},
		{"send-to-stdout", b.SendToStdOut && !b.SendToStdErr},
		{"-m", b.MarkStdOut != nil && *b.MarkStdOut == "x"},
		{"-n", b.NextLine},
		{"-s number", b.Signal != nil && *b.Signal == syscall.SIGKILL},
		{"-i", b.Input != nil && *b.Input == "y"},
		{"-f", b.InputFile != nil && *b.InputFile == "z"},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("command b: %s not parsed as expected: %+v", c.name, b)
		}
	}
	if ec := o.Commands[2].Actions.SetExitCode; ec == nil || *ec != 0 {
		t.Errorf("command c: -e 0 not parsed: %v", ec)
	}
}

func TestDeadline(t *testing.T) {
	t.Run("appends an implicit last command", func(t *testing.T) {
		o := mustParse(t, withTail("--deadline", "30s", "-c", "-p", "x")...)
		if len(o.Commands) != 2 {
			t.Fatalf("got %d commands", len(o.Commands))
		}
		d := o.Commands[1]
		if d.Conditions.Timeout == nil || *d.Conditions.Timeout != 30*time.Second || !d.IsTimed() {
			t.Errorf("deadline command conditions = %+v", d.Conditions)
		}
		if d.Actions.Exit == nil || *d.Actions.Exit != 1 {
			t.Errorf("deadline command Exit = %v", d.Actions.Exit)
		}
		if o.Deadline == nil || *o.Deadline != 30*time.Second {
			t.Errorf("Deadline = %v", o.Deadline)
		}
	})
	t.Run("is enough on its own", func(t *testing.T) {
		o := mustParse(t, "--deadline", "5s", "--no-stdbuf", "--", "true")
		if len(o.Commands) != 1 || !o.Commands[0].IsTimed() {
			t.Errorf("commands = %+v", o.Commands)
		}
	})
	t.Run("invalid duration", func(t *testing.T) {
		_, err := parse(t, withTail("--deadline", "soon", "-c")...)
		if err == nil || !strings.Contains(err.Error(), "invalid duration") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestExitAction(t *testing.T) {
	o := mustParse(t, withTail("-c", "-x", "0", "-c", "--exit", "255")...)
	for i, want := range []int32{0, 255} {
		e := o.Commands[i].Actions.Exit
		if e == nil || *e != want {
			t.Errorf("command %d: Exit = %v, want %d", i, e, want)
		}
	}
	if o.Commands[0].Actions.Signal != nil || o.Commands[0].Actions.SetExitCode != nil {
		t.Errorf("--exit must not set Signal or SetExitCode directly")
	}
}

func TestSignalParsing(t *testing.T) {
	cases := []struct {
		arg  string
		want syscall.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"sigterm", syscall.SIGTERM},
		{"SigUsr2", syscall.SIGUSR2},
		{"TERM", syscall.SIGTERM},
		{"int", syscall.SIGINT},
		{"Usr1", syscall.SIGUSR1},
		{"KILL", syscall.SIGKILL},
		{"15", syscall.SIGTERM},
		{"2", syscall.SIGINT},
	}
	for _, c := range cases {
		t.Run(c.arg, func(t *testing.T) {
			o := mustParse(t, withTail("-c", "-s", c.arg)...)
			got := o.Commands[0].Actions.Signal
			if got == nil || *got != c.want {
				t.Errorf("Signal = %v, want %v", got, c.want)
			}
		})
	}
}

func TestColorParsing(t *testing.T) {
	t.Run("no color action leaves Color nil", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "-p", "x")...)
		if o.Commands[0].Actions.Color != nil {
			t.Errorf("Color should be nil")
		}
	})
	names := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
		"hi-black", "hi-red", "hi-green", "hi-yellow", "hi-blue", "hi-magenta", "hi-cyan", "hi-white", "RED"}
	for _, n := range names {
		t.Run("fg "+n, func(t *testing.T) {
			o := mustParse(t, withTail("-c", "--fg-color", n)...)
			if o.Commands[0].Actions.Color == nil {
				t.Errorf("Color is nil")
			}
		})
		t.Run("bg "+n, func(t *testing.T) {
			o := mustParse(t, withTail("-c", "--bg-color", n)...)
			if o.Commands[0].Actions.Color == nil {
				t.Errorf("Color is nil")
			}
		})
	}
	for _, flag := range []string{"--bold", "--italic", "--faint", "--underline", "--blink", "--blink-rapid"} {
		t.Run(flag, func(t *testing.T) {
			o := mustParse(t, withTail("-c", flag)...)
			if o.Commands[0].Actions.Color == nil {
				t.Errorf("Color is nil")
			}
		})
	}
	t.Run("attributes accumulate into one color", func(t *testing.T) {
		o := mustParse(t, withTail("-c", "--fg-color", "red", "--bold", "--bg-color", "blue")...)
		c := o.Commands[0].Actions.Color
		if c == nil {
			t.Fatal("Color is nil")
		}
		c.EnableColor()
		s := c.Sprint("x")
		for _, code := range []string{"31", "1", "44"} {
			if !strings.Contains(s, code) {
				t.Errorf("escape sequence %q lacks attribute %s", s, code)
			}
		}
	})
}

func TestCommandState(t *testing.T) {
	o := mustParse(t, withTail("-c", "--disabled", "-c", "--line-disabled", "-c", "--line-enabled", "-c")...)
	want := []struct{ d, ld, le bool }{{true, false, false}, {false, true, false}, {false, false, true}, {false, false, false}}
	for i, w := range want {
		c := o.Commands[i]
		if c.Disabled != w.d || c.LineDisabled != w.ld || c.LineEnabled != w.le {
			t.Errorf("command %d: %+v, want %+v", i, c, w)
		}
	}
}

func TestToggleOtherCommandsIsAllowed(t *testing.T) {
	o := mustParse(t, withTail("-c", "a", "--toggle", "b", "--toggle", "c", "-c", "b", "-c", "c", "--toggle", "a")...)
	if got := o.Commands[0].Actions.Toggle; len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("Toggle = %v", got)
	}
}

func TestSkipToForwardIsAllowed(t *testing.T) {
	o := mustParse(t, withTail("-c", "a", "--skip-to", "c", "-c", "b", "-c", "c")...)
	if s := o.Commands[0].Actions.SkipTo; s == nil || *s != "c" {
		t.Errorf("SkipTo = %v", s)
	}
}

func TestParseErrors(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "exists.pid")
	if err := os.WriteFile(pidFile, []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		want string // substring of the error message
	}{
		{"unknown option", withTail("--bogus", "-c"), "invalid command"},
		{"no --", []string{"-c"}, "you must specify --"},
		{"-- without program", []string{"-c", "--"}, "you must specify --"},
		{"program not found", []string{"--no-stdbuf", "-c", "--", "no-such-program-xyz"}, "not found"},
		{"no command", tail, "at least one command"},
		{"share-streams with share-commands", withTail("--share-streams", "--share-commands", "-c"), "cannot combine"},
		{"line buffer too small", withTail("--line-buffer-size", "512", "-c"), "at least 1024"},
		{"line buffer not int", withTail("--line-buffer-size", "big", "-c"), "must be an int"},
		{"pid file exists", withTail("--pid", pidFile, "-c"), "already exists"},
		{"empty command name", withTail("-c", ""), "cannot be empty"},
		{"duplicate command name", withTail("-c", "x", "-c", "x"), "duplicate command name"},
		{"missing option value", []string{"-c", "--set-prefix"}, "missing value"},
		{"empty pattern", withTail("-c", "-p", ""), "pattern must not be empty"},
		{"invalid regexp", withTail("-c", "-p", "("), "error parsing regexp"},
		{"--or without pattern", withTail("-c", "--or"), "--or without"},
		{"two state flags", withTail("-c", "--disabled", "--line-enabled"), "only use one of"},
		{"share-streams with --std-err", withTail("--share-streams", "-c", "--std-err"), "--std-err or --std-all"},
		{"share-streams with --std-all", withTail("--share-streams", "-c", "-a"), "--std-err or --std-all"},
		{"share-streams with --mark-stderr", withTail("--share-streams", "-c", "--mark-stderr", "x"), "--mark-stderr"},
		{"invalid duration", withTail("-c", "--no-input-for", "soon"), "invalid duration"},
		{"--timeout with pattern", withTail("-c", "-p", "x", "-t", "1s"), "cannot be combined with pattern"},
		{"--timeout with --std-err", withTail("-c", "-t", "1s", "--std-err"), "cannot use --std-err or --std-all"},
		{"--no-input-for with --std-all", withTail("-c", "--no-input-for", "1s", "-a"), "cannot use --std-err or --std-all"},
		{"--timeout with --mark", withTail("-c", "-t", "1s", "-m", "."), "no 'current line'"},
		{"--or-timeout not implemented", withTail("-c", "--or-timeout", "1s"), "not implemented"},
		{"--min-match-time not implemented", withTail("-c", "--min-match-time", "1s"), "not implemented"},
		{"two timed conditions", withTail("-c", "--no-input-for", "1s", "-t", "1s"), "single timeout"},
		{"timed command with pattern", withTail("-c", "-p", "x", "--no-input-for", "1s"), "cannot be combined with pattern"},
		{"timed command with --mark", withTail("-c", "--no-input-for", "1s", "-m", "."), "no 'current line'"},
		{"timed command with --set-prefix", withTail("-c", "--no-input-for", "1s", "--set-prefix", "p"), "no 'current line'"},
		{"timed command with --send-to-stderr", withTail("-c", "--no-input-for", "1s", "--send-to-stderr"), "no 'current line'"},
		{"timed command with color", withTail("-c", "--no-input-for", "1s", "--bold"), "no 'current line'"},
		{"send-to both", withTail("-c", "--send-to-stdout", "--send-to-stderr"), "cannot be combined"},
		{"exit code too big", withTail("-c", "-e", "256"), "between 0 and 255"},
		{"exit code negative", withTail("-c", "-e", "-1"), "between 0 and 255"},
		{"exit code not int", withTail("-c", "-e", "x"), "must be an int"},
		{"set and clear exit code", withTail("-c", "-e", "1", "--clear-exit-code"), "cannot be combined"},
		{"--exit too big", withTail("-c", "-x", "256"), "between 0 and 255"},
		{"--exit not int", withTail("-c", "--exit", "ok"), "must be an int"},
		{"--exit with -e", withTail("-c", "-x", "0", "-e", "1"), "--exit cannot be combined"},
		{"--exit with --clear-exit-code", withTail("-c", "-x", "0", "--clear-exit-code"), "--exit cannot be combined"},
		{"--exit with -s", withTail("-c", "-x", "0", "-s", "SIGINT"), "--exit cannot be combined"},
		{"--stop-signal invalid", withTail("--stop-signal", "SIGNOPE", "-c"), "signal name or a signal number"},
		{"unknown signal name", withTail("-c", "-s", "SIGNOPE"), "signal name or a signal number"},
		{"invalid signal number", withTail("-c", "-s", "999"), "invalid signal number"},
		{"invalid fg color", withTail("-c", "--fg-color", "pink"), "invalid color name"},
		{"invalid bg color", withTail("-c", "--bg-color", "pink"), "invalid color name"},
		{"--disable unknown", withTail("-c", "--disable", "nope"), "cannot find command"},
		{"--enable unknown", withTail("-c", "--enable", "nope"), "cannot find command"},
		{"--toggle unknown", withTail("-c", "--toggle", "nope"), "cannot find command"},
		{"--toggle self", withTail("-c", "a", "--toggle", "a"), "cannot toggle itself"},
		{"name starting with dash", withTail("-c", "a", "--disable", "-a"), "cannot start with '-'"},
		{"disable and enable same", withTail("-c", "a", "--disable", "a", "--enable", "a"), "both --disable and --enable"},
		{"disable and toggle same", withTail("-c", "a", "--disable", "a", "--toggle", "a"), "both --disable and --toggle"},
		{"enable and toggle same", withTail("-c", "a", "--enable", "a", "--toggle", "a"), "both --enable and --toggle"},
		{"--skip-to unknown", withTail("-c", "--skip-to", "nope"), "cannot find command"},
		{"--skip-to backward", withTail("-c", "a", "-c", "b", "--skip-to", "a"), "previous command"},
		{"--skip-to self", withTail("-c", "a", "--skip-to", "a"), "itself"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parse(t, c.args...)
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

func TestErrorMentionsCommand(t *testing.T) {
	_, err := parse(t, withTail("-c", "-c", "named", "--or")...)
	if err == nil || !strings.Contains(err.Error(), "command #2 (name=named)") {
		t.Errorf("error = %v", err)
	}
	_, err = parse(t, withTail("-c", "--or")...)
	if err == nil || !strings.Contains(err.Error(), "command #1:") {
		t.Errorf("error = %v", err)
	}
}

func TestCloneIsIndependent(t *testing.T) {
	o := mustParse(t, withTail("-c", "a", "-p", "x", "--disable", "a", "-c", "b")...)
	cp := CloneCommands(o.Commands)
	cp[0].Disabled = true
	cp[0].Actions.Disable[0] = "changed"
	cp[0].Conditions.RawPatterns[0] = "changed"
	cp[0].Conditions.StdErr = true
	if o.Commands[0].Disabled {
		t.Errorf("Disabled leaked into the original")
	}
	if o.Commands[0].Actions.Disable[0] != "a" {
		t.Errorf("Actions.Disable leaked into the original")
	}
	if o.Commands[0].Conditions.RawPatterns[0] != "x" || o.Commands[0].Conditions.StdErr {
		t.Errorf("Conditions leaked into the original")
	}
	if cp[0].Conditions.CompiledPatterns[0] != o.Commands[0].Conditions.CompiledPatterns[0] {
		t.Errorf("compiled regexps should be shared, not recompiled")
	}
}
