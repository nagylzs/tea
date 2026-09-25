# Changelog

All notable changes to tea are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/),
the version numbers follow [Semantic Versioning](https://semver.org/). Release notes on GitHub are the same text:
the section for a version is the annotation of its tag.

## [2.0.0] - 2026-09-25

tea runs a program, watches its output line by line, and acts on what it prints. This release makes the
documented behaviour real, adds the missing halves of the tool (input, timeouts) and shortens the grammar.
It is a breaking release; see the last section before upgrading scripts.

### Highlights

- **Shortcuts for the common jobs.** The first command no longer needs `-c`. `--exit CODE` stops the program
  and sets tea's exit code. `--deadline DURATION` gives up after a time limit. Signal names work without the
  `SIG` prefix. The three headline uses are now one-liners:

      tea -p "operation completed" -s INT -- command
      tea -p "ready to accept connections" --exit 0 --deadline 120s -- docker logs -f pg
      tea --no-input-for 10s --exit 0 -- tail -f access.log

- **`--timeout DURATION`** is implemented: a deadline counted from when the command was last enabled, so it can
  also be relative to an event seen in the output.
- **Standard input works.** tea forwards its own stdin to the program, and `--send-input`, `--send-input-file`
  and `--close` reach the program in order through one queue. `--no-stdin` turns forwarding off.
- **Output buffering is documented per runtime** (glibc, Python, Perl, Ruby, Go, Rust, Node.js, Java, shell) in
  `tea --help`, and tea no longer refuses to start when `stdbuf` is not installed.
- **Rewritten `--help` and README**, a test suite (parser and end-to-end), CI on every push, and this release
  built by CI for linux/amd64 and linux/arm64 as static binaries.

### Fixed

- `--disable`, `--enable` and `--toggle` state is now independent per stream, as always documented.
- `--line-enabled` and `--line-disabled` had no effect.
- Timed commands fired once per stream chain (an idle command with `-i` sent its input twice) and `--no-input-for`
  fired when only stderr was quiet. There is now one instance of each timed command, evaluated by one timer.
- Colors were not applied to the line itself on stderr. Each output line is now written in one piece, so lines
  from the two streams cannot be interleaved mid-line on a shared terminal.
- A lone `--timeout` was silently accepted and matched every line.
- `--skip-to` could target its own command and loop forever; `--toggle` of the own command was accepted.
- Invalid color names were ignored; an unknown `--toggle` target crashed at run time.
- A line longer than `--line-buffer-size` was silently dropped with everything after it; it is now an error.
- `--mark` combined with `--send-to-stderr` printed the full line instead of the mark.
- `--version` prints a parseable build time.

### Breaking changes

- `--no-input-for-duration` is renamed to `--no-input-for`, with no alias.
- `--no-input-for` fires once per quiet period instead of once per idle second.
- tea reads its own stdin and forwards end of file. Add `--no-stdin` when tea runs in the background of an
  interactive shell, or when its stdin is already at end of file and you rely on `--send-input`.
- `--disable`, `--enable` and `--toggle` no longer leak between the stdout and stderr chains; use
  `--share-commands` for one chain.
- A lone `--timeout`, `--or-timeout` or `--min-match-time` used to be accepted; `--timeout` now works as
  documented, the other two are rejected as not implemented.
- `--skip-to` and `--toggle` may not target their own command; timed commands reject `--std-err` and `--std-all`.
- The child's exit status message is printed to stderr instead of stdout.
- A line longer than `--line-buffer-size` is a fatal error.

## [1.0.0] - 2024-06-23

Pre-release. Line processing, pattern conditions, output rewriting and coloring, signals and exit codes,
enable/disable/toggle and branching between commands. Time based conditions and input to the program were parsed
but not implemented.

[2.0.0]: https://github.com/nagylzs/tea/releases/tag/v2.0.0
[1.0.0]: https://github.com/nagylzs/tea/releases/tag/latest
