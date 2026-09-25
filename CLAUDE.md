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
go build -o tea ./cmd/tea           # plain build
go vet ./...
python3 scripts/build.py [ARCH...]  # static release build -> dist/linux/<arch>/tea (default amd64 arm64); injects
                                    # Version/Commit/Branch/Built into internal/version via -ldflags -X; --debug keeps symbols
./tea --help                        # prints USAGE.txt
./tea --version                     # "tea dev (commit unset, ...)" unless built via scripts/build.py
```

CI (`.github/workflows/go.yml`) vets, tests and runs the build script on every push; `release.yml` does the same on a
`v*` tag and attaches `tea-linux-{amd64,arm64}` plus `SHA256SUMS` to a GitHub release.

```bash
go test ./...                       # what CI runs (after go vet); needs python3 and stdbuf on PATH
go test ./internal/...              # parser + validator only, fast
go test ./test/ -run TestSignal -v  # one end-to-end test
```

Tests live in two places:

- `internal/opts/parser_test.go` — table-driven tests of the argument parser and validator. They reset the
  package globals (`Opts`, `argIdx`, `cmdIdx`) and set `os.Args` directly, so keep them in package `opts`.
- `test/e2e_test.go` (package `e2e`) — end-to-end tests. `TestMain` builds the binary into a temp dir and every test
  runs it against a python child. By default tea's stdin is a pipe that is never written (an idle terminal);
  `stdinData`/`stdinNull` model piped input and `/dev/null`. Children: `test/emit.py` (scriptable stdout/stderr/sleep/exit/stdin tokens), `test/signals.py`
  (prints received signals, exits on SIGTERM) and `test/withpty.py` (runs tea under a pseudo-terminal, needed for the
  color tests because fatih/color disables itself when stdout is not a TTY). Cross-stream ordering is not
  deterministic, so tests that depend on it put `sleep:` tokens between lines.

Not covered because not implemented: `--or-timeout`, `--min-match-time`.

`go.mod` declares `go 1.25`; the code relies on Go 1.23+ `time.Timer.Reset` semantics (no manual channel draining).

## Architecture

Two packages:

- `internal/opts` — hand-rolled argument parser (no `flag`/cobra). `parser.go` walks `os.Args` with a global `argIdx`,
  maps option strings to an `Option` enum, and mutates the package-global `Opts`. Options are either *global* (allowed
  anywhere; see `isGlobalOption`) or *command-level* (mutate `currentCommand()`, the last `-c` started; the first
  command-level option before any `-c` starts command #1 implicitly).
  `pop.go` holds the `popXxxArg` helpers that consume the next argv value (durations, signals, colors, names).
  `validate.go` runs after parsing: compiles regexes, builds `Opts.CmdIdx` (name → index) and rejects invalid
  combinations. `--deadline` becomes an implicit last command (`--timeout D --exit 1`) there. **New options need entries in the `Option` enum, `longOptions`/`shortOptions`, the `switch` in
  `internalParseArgs`, and usually a check in `validateCommand`.**
- `cmd/tea/tea.go` — runtime. Unless `--no-stdbuf` (or stdbuf is not on PATH, which only warns), PROGRAM is wrapped as
  `stdbuf -oL -eL PROGRAM ...` to force
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

`main()` builds one `Chain` (`[]*opts.Command`) of the parsed commands and, in the default two-chain mode, derives a
chain per stream with `newChain`: line commands are deep-copied (per-stream `--disable`/`--enable`/`--toggle` state, as
USAGE.txt promises), timed commands (`Command.IsTimed()`: `--timeout` or `--no-input-for`) are the same shared
instance in every chain. All mutable command state is guarded by the global `stateMu`; a processor holds it while
evaluating a line and releases it before writing output.

`processLine` is the per-line loop: records `lastLine[stream]`, applies `--line-enabled/--line-disabled`, then for each
command skips timed ones, checks `Disabled`, the stream filter (`Conditions.StdOut/StdErr`) and `commandLineMatch`
(AND/OR/NO pattern logic), then applies actions. "Last one wins" semantics for mark/prefix/suffix/color come from
overwriting fields on the `Line`. `--next-line` breaks the loop; `--skip-to` sets `cmdIdx` forward.

`RunTimers` is a single goroutine, stopped before the child is reaped, that ticks once per second and calls
`evaluateTimers`: `--no-input-for` fires once when `now - max(lastLine)` reaches the duration (`Fired`, cleared by
`processLine` when a line arrives);
`--timeout` fires once when `now - Started` reaches the duration, then sets `Fired`. `setDisabled` is the only way
state changes: enabling a disabled command resets `Started` and `Fired`, which is what makes a `--timeout` count
from the last enable. Timed commands' state-changing actions go to every chain (`targets` dedupes the shared
instances), a line command's only to its own chain.

Both paths share `applyActions` for every action that does not touch the current line (signal, stdin, exit code,
enable/disable/toggle, `--next-line`, `--skip-to`). Line-specific actions (mark/prefix/suffix/color/send-to) live only
in `processLine`. A new action goes in `applyActions` unless it needs the `Line`.

The child's stdin has one owner, the `WriteStdIn` goroutine, fed through `chStdInIn` by three producers:
`--send-input`/`--send-input-file`/`--close` from the processors, and `ForwardStdIn`, which copies tea's own stdin in
raw chunks and queues a close at EOF (skipped with `--no-stdin`). Requests pass through `unboundedQueue`, so a
processor never blocks on the child's stdin (which could deadlock against the child's stdout). The close is queued
after the line's inputs. Once stdin is closed or a write fails, `--send-input` data is dropped with a warning and
forwarded data silently. The writer never exits; `main()` sends a `done` flush marker and waits for it before
`cmd.Wait()`.

`--send-input-file` reads the file in `applyActions` when the action fires and queues the contents the same way.

`--or-timeout` and `--min-match-time` are parsed and rejected in `validate.go` as not implemented.
