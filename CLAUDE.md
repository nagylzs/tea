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

There are no Go tests. `test/` (git-ignored) holds sample log data and `test/test03.py`, a script that endlessly
writes lines to stderr with a 0.2s delay — useful for manually exercising stderr handling and
`--no-input-for-duration`, e.g. `go run cmd/tea/tea.go -c --std-err -m . -- python3 -u test/test03.py`.

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

- **default**: two `ProcessLines` goroutines, one per stream, each given what is *intended* to be its own copy of
  the `[]opts.Command` chain (`cmdStdOut := o.Commands` / `cmdStdErr := o.Commands`) so that
  `--disable`/`--enable`/`--toggle` state is independent per stream (as USAGE.txt promises). `FixedExitCode`
  (an `atomic.Int32`, `-1` = "use child's exit code") is deliberately shared.
- `--share-commands`: stdout and stderr lines are merged into one channel and processed by one chain.
- `--share-streams`: both pipes are read into the stdout channel; every line looks like stdout.

Caveat: those "copies" are slice-header copies sharing one backing array, and `Command.Actions`/`Conditions` are
pointers, so in practice the two chains mutate the same `Disabled` flags and the same structs. Anything relying on
per-stream isolation needs a real deep copy.

### Command evaluation

`processLine` is the per-line loop: applies `--line-enabled/--line-disabled`, then for each command checks
`Disabled`, stream filter (`Conditions.StdOut/StdErr`), and `commandLineMatch` (AND/OR/NO pattern logic), then applies
actions. "Last one wins" semantics for mark/prefix/suffix/color come from simply overwriting fields on the `Line` as
commands run. `--next-line` breaks the loop; `--skip-to` sets `cmdIdx` forward.

`processTimedCommands` is a near-duplicate of the action section of `processLine`, driven by a 1-second idle timer in
`ProcessLines`. It only handles `--no-input-for-duration` (patternless, "no current line" commands). If you add a new
action, add it to **both** functions.

`--timeout`, `--or-timeout`, `--min-match-time` are parsed and rejected in `validate.go` as not implemented; a design
sketch for them is in the big comment inside `processLine`. `--send-input-file` parses but `log.Fatal`s at runtime.
Output writing to the child's stdin (`chStdInIn`) is a 1-slot buffered channel with the consumer commented out in
`main()`, so a second `--send-input` action would block — this is a known unfinished area.
