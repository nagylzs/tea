# TODO

## Ergonomics

- [x] The command-chain grammar is dense. Done: the first command is implicit (no `-c`), `--no-input-for` fires
  once per quiet period (no self-disable dance), `--exit CODE` replaces `-s SIG -e CODE`, `--deadline D` replaces the
  trailing `-c -t D --exit 1` command, and signal names work without the `SIG` prefix. The three common uses are now:
  - `tea -p PATTERN -s INT -- ...`
  - `tea -p PATTERN --exit 0 --deadline 120s -- ...`
  - `tea --no-input-for 10s --exit 0 -- ...`
- [ ] Document per-runtime output buffering in USAGE.txt. `stdbuf` only affects glibc stdio; Java, Go, Node,
  and Python (without `-u` / `PYTHONUNBUFFERED`) ignore it.

## Unfinished features

- [x] Wire `--send-input` to the child's stdin. Done: `WriteStdIn` goroutine with an unbounded queue, `--close`
  goes through the same queue so ordering is preserved.
- [x] Implement `--send-input-file`. Done: read at execution time, queued like `--send-input`.
- [x] Implement `--timeout`. Done: patternless deadline counted from the last enable, fired once; timed commands are
  now single instances evaluated by one timer goroutine, so they fire once, not once per chain.
- [ ] `--or-timeout` and `--min-match-time` are parsed and rejected. The pattern-and-timeout semantics from the old
  design were dropped: a pattern command plus a patternless `--timeout` command express the same thing more clearly.
  Implement only if a concrete use case appears.
- [x] Forward tea's own stdin to the child. Done: `ForwardStdIn`, raw chunks, EOF closes the child's stdin,
  `--no-stdin` opts out.

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
- [x] USAGE.txt said `--toggle` cannot toggle its own command, but nothing enforced it. Now rejected by the validator.

## Tests

- [x] Table-driven tests for `internal/opts` (parser + validator) in `internal/opts/parser_test.go`, end-to-end tests
  in `test/e2e_test.go` driving python children. See CLAUDE.md.
