# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`tea` is a single-binary Go CLI (Linux-only: uses `syscall.Kill`, `golang.org/x/sys/unix`, and `stdbuf(1)`) that starts a
child PROGRAM, reads its stdout/stderr line by line, and runs a user-defined chain of "commands" (conditions + actions)
per line — sending signals, writing to the child's stdin, rewriting/coloring output, or overriding the exit code.

```
tea COMMAND [COMMAND...] -- PROGRAM [ARG...]
```

The full option reference lives in `cmd/tea/USAGE.txt`, which is `//go:embed`-ed and printed by `--help`. **Any change to
an option's name or behaviour must be mirrored there** — it is the only user-facing documentation.

## Commands

```bash
go build -o tea cmd/tea/tea.go      # plain build (what CI does)
go vet ./...
python3 scripts/build.py            # release build -> dist/<os>/<arch>/tea and tea_debug; runs `go mod tidy`
                                    # and injects Built/Commit/Branch into internal/version via -ldflags -X
./tea --help                        # prints USAGE.txt
./tea --version                     # "unset unset unset" unless built via scripts/build.py
```

```bash
go test ./...                       # what CI runs (after go vet); needs python3 and stdbuf on PATH
go test ./internal/...              # parser + validator only, fast
go test ./test/ -run TestSignal -v  # one end-to-end test
```

Tests live in two places:

- `internal/opts/parser_test.go` — table-driven tests of the argument parser and validator. They reset the
  package globals (`Opts`, `argIdx`, `cmdIdx`) and set `os.Args` directly, so keep them in package `opts`.
- `test/e2e_test.go` (package `e2e`) — end-to-end tests. `TestMain` builds the binary into a temp dir and every test
  runs it against a python child: `test/emit.py` (scriptable stdout/stderr/sleep/exit/stdin tokens), `test/signals.py`
  (prints received signals, exits on SIGTERM) and `test/withpty.py` (runs tea under a pseudo-terminal, needed for the
  color tests because fatih/color disables itself when stdout is not a TTY). Cross-stream ordering is not
  deterministic, so tests that depend on it put `sleep:` tokens between lines.

Not covered because not implemented: `--timeout`, `--or-timeout`, `--min-match-time`, stdin forwarding.

`go.mod` declares `go 1.25`; the code relies on Go 1.23+ `time.Timer.Reset` semantics (no manual channel draining).

## Architecture

Two packages:

- `internal/opts` — hand-rolled argument parser (no `flag`/cobra). `parser.go` walks `os.Args` with a global `argIdx`,
  maps option strings to an `Option` enum, and mutates the package-global `Opts`. Options are either *global* (must
  appear before any `-c`; see `isGlobalOption`) or *command-level* (mutate `currentCommand()`, the last `-c` started).
  `pop.go` holds the `popXxxArg` helpers that consume the next argv value (durations, signals, colors, names).
  `validate.go` runs after parsing: compiles regexes, builds `Opts.CmdIdx` (name → index) and rejects invalid
  combinations. **New options need entries in the `Option` enum, `longOptions`/`shortOptions`, the `switch` in
  `internalParseArgs`, and usually a check in `validateCommand`.**
- `cmd/tea/tea.go` — runtime. Unless `--no-stdbuf`, PROGRAM is wrapped as `stdbuf -oL -eL PROGRAM ...` to force
  line-buffered child output.

### Data flow (tea.go)

`ReadLines` goroutines scan the child's stdout/stderr into `Line` structs → `ProcessLines` goroutine(s) run the
command chain → `WriteData` goroutines write strings to tea's own stdout/stderr. All hand-offs are channels;
`sync.WaitGroup`s chain the closes so writers exit after processors, which exit after readers.

The three stream modes differ only in how channels are wired in `main()`:

- **default**: two `ProcessLines` goroutines, one per stream, each given its own deep copy of the
  `[]opts.Command` chain (`opts.CloneCommands`) so that `--disable`/`--enable`/`--toggle` state is independent per
  stream (as USAGE.txt promises). `FixedExitCode` (an `atomic.Int32`, `-1` = "use child's exit code") is
  deliberately shared.
- `--share-commands`: stdout and stderr lines are merged into one channel and processed by one chain.
- `--share-streams`: both pipes are read into the stdout channel; every line looks like stdout.

### Command evaluation

`processLine` is the per-line loop: applies `--line-enabled/--line-disabled`, then for each command checks
`Disabled`, stream filter (`Conditions.StdOut/StdErr`), and `commandLineMatch` (AND/OR/NO pattern logic), then applies
actions. "Last one wins" semantics for mark/prefix/suffix/color come from simply overwriting fields on the `Line` as
commands run. `--next-line` breaks the loop; `--skip-to` sets `cmdIdx` forward.

`processTimedCommands` is driven by a 1-second idle timer in `ProcessLines` and only handles
`--no-input-for` (patternless, "no current line" commands). Both it and `processLine` call the shared
`applyActions` for every action that does not touch the current line (signal, stdin, exit code, enable/disable/toggle,
`--next-line`, `--skip-to`). Line-specific actions (mark/prefix/suffix/color/send-to) live only in `processLine`. A new
action goes in `applyActions` unless it needs the `Line`.

`--send-input` and `--close` are `stdInRequest`s sent on `chStdInIn`; one `WriteStdIn` goroutine serves them in
order through `unboundedQueue`, so a processor never blocks on the child's stdin (which could deadlock against the
child's stdout). The close is queued after the line's inputs. Writes to an already closed stdin log a warning.
`main()` closes `chStdInIn` after the processors finish and waits for the writer before `cmd.Wait()`.

`--send-input-file` reads the file in `applyActions` when the action fires and queues the contents the same way.

`--timeout`, `--or-timeout`, `--min-match-time` are parsed and rejected in `validate.go` as not implemented; a design
sketch for them is in the big comment inside `processLine`. Forwarding tea's own stdin (`ReadStdIn`, commented out in `main()`) would be another producer on `chStdInIn`.
