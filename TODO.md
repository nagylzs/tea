# TODO

## Ergonomics

- [ ] The command-chain grammar is dense. Consider first-class shortcuts for the three most common uses:
  - wait for pattern, then signal (`tea -c -p PATTERN -s SIG -- ...`)
  - wait for pattern with a deadline, non-zero exit if it doesn't appear
  - quiet-for-N-seconds, then act (`-c NAME --no-input-for 10s --disable NAME`)
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

- [x] A lone `--timeout`/`--or-timeout`/`--min-match-time` was accepted and then matched every line (only the
  combination of two was rejected). Now rejected as not implemented.
- [x] `--skip-to` could target its own command, looping forever on a matching line.
- [x] A line longer than `--line-buffer-size` was silently dropped together with all later output. Now fatal.
- [x] The child's exit status message was printed to stdout. Now stderr.
- [x] Invalid `--fg-color`/`--bg-color` names were accepted (shadowed error variable).
- [x] `--toggle NAME` with an unknown NAME passed validation and crashed at runtime.
- [x] `--mark` together with `--send-to-stderr` (or `--mark-stderr` with `--send-to-stdout`) picked the mark by
  *output* stream; USAGE.txt describes it by *input* stream. The code now follows the docs.
- [ ] USAGE.txt says `--toggle` cannot toggle its own command, but nothing enforces it (it works, toggling itself off).

## Tests

- [x] Table-driven tests for `internal/opts` (parser + validator) in `internal/opts/parser_test.go`, end-to-end tests
  in `test/e2e_test.go` driving python children. See CLAUDE.md.
