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

- [x] Per-stream command "copies" were slice-header copies sharing one backing array, so `--disable`/`--enable`/
  `--toggle` state leaked between the stdout and stderr chains. Fixed: `opts.CloneCommands` deep-copies the chain.
- [x] `processTimedCommands` duplicated the action section of `processLine`. Fixed: shared `applyActions`.
- [x] `--line-enabled`/`--line-disabled` ranged over struct copies, so the per-line reset never took effect.
- [x] Color was not applied to the line value when writing to stderr (USAGE.txt promises both streams).

## Tests

- [ ] No Go tests. Add table-driven tests for `internal/opts` (parser + validator) and end-to-end tests that run
  `tea` against a small script (e.g. `test/test03.py`) for signal, exit-code and idle-timeout behaviour.
