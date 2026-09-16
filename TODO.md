# TODO

## Ergonomics

- [ ] The command-chain grammar is dense. Consider first-class shortcuts for the three most common uses:
  - wait for pattern, then signal (`tea -c -p PATTERN -s SIG -- ...`)
  - wait for pattern with a deadline, non-zero exit if it doesn't appear
  - quiet-for-N-seconds, then act (`-c NAME --no-input-for-duration 10s --disable NAME`)
- [ ] Document per-runtime output buffering in USAGE.txt. `stdbuf` only affects glibc stdio; Java, Go, Node,
  and Python (without `-u` / `PYTHONUNBUFFERED`) ignore it.

## Unfinished features

- [ ] Wire `--send-input` to the child's stdin. `chStdInIn` is a 1-slot buffered channel and its consumer
  (`WriteData(m.StdIn, chStdInIn, nil)`) is commented out in `main()`, so a second `--send-input` blocks.
- [ ] Implement `--send-input-file` (currently `log.Fatal`s at runtime).
- [ ] Implement `--timeout`, `--or-timeout`, `--min-match-time` (parsed, rejected in `validate.go`). Design sketch is
  in the comment inside `processLine()`. Decide whether both per-stream command chains should fire.
- [ ] Forward tea's own stdin to the child (`ReadStdIn` call is commented out in `main()`).

## Correctness

- [ ] Per-stream command "copies" (`cmdStdOut := o.Commands` / `cmdStdErr := o.Commands`) are slice-header copies
  sharing one backing array, and `Actions`/`Conditions` are pointers. `--disable`/`--enable`/`--toggle` state is
  therefore shared between stdout and stderr chains, contrary to what USAGE.txt promises. Needs a deep copy.
- [ ] `processTimedCommands` duplicates the action section of `processLine`; extract a shared `applyActions`.

## Tests

- [ ] No Go tests. Add table-driven tests for `internal/opts` (parser + validator) and end-to-end tests that run
  `tea` against a small script (e.g. `test/test03.py`) for signal, exit-code and idle-timeout behaviour.
