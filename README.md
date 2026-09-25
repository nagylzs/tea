# tea

[![CI](https://github.com/nagylzs/tea/actions/workflows/go.yml/badge.svg)](https://github.com/nagylzs/tea/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nagylzs/tea.svg)](https://pkg.go.dev/github.com/nagylzs/tea)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

**Run a program, watch its output line by line, and act on what it prints.**

`tea` starts a program as its child, reads its stdout and stderr, and runs a chain of small commands on every line.
A command matches lines with regular expressions and then does something: sends a signal to the program, writes to
its stdin, rewrites or colors the line, or decides the exit code. Lines that nothing touches pass through unchanged,
so `tea` can sit in front of any program.

```sh
# wait until the server says it is ready, then stop watching and exit 0;
# if that does not happen within two minutes, exit 1 instead
tea -p "ready to accept connections" --exit 0 --deadline 120s -- docker logs -f pg
```

The name is a nod to `tee`: like `tee`, it sits in the middle of a data stream. Unlike `tee`, the stream is the
output of a process that `tea` started itself, which is what makes acting on the process easy.

## Why

Shell scripts that need to react to a program's output tend to end up as `prog | while read line; do ...; done`
loops that cannot signal the program because the pipeline hid its pid, or as `expect` scripts for a job that has
nothing to do with terminals. `tea` covers the common cases in one line, with a single static binary and no pty:

| Job | Command |
|---|---|
| Wait for a pattern, then interrupt the program | `tea -p "operation completed" -s INT -- command` |
| Readiness wait with a deadline and a meaningful exit code | `tea -p ready --exit 0 --deadline 120s -- command` |
| Act when the program has been quiet for a while | `tea --no-input-for 10s --exit 0 -- tail -f access.log` |
| Answer a prompt | `tea --no-stdin -p "^Continue\?" -i $'y\n' -- ./installer` |
| Run with a time limit | `tea --deadline 1h -- ./long-job` |
| Signal 30 s after an event, not before | `tea -p "phase 2" --enable d -c d --disabled -t 30s -s INT -- command` |
| Color error lines red | `tea --std-err --fg-color red -- ./job` |
| One dot per line instead of the noise | `tea -m . -- ./chatty-job` |

## Install

Linux only: `tea` uses Linux signals and `stdbuf` from GNU coreutils.

With Go 1.25 or newer:

```sh
go install github.com/nagylzs/tea/cmd/tea@latest
```

Or download a binary for amd64 or arm64 from the [releases page](https://github.com/nagylzs/tea/releases); releases
are built by CI when a `v*` tag is pushed. Every push to `main` also leaves both binaries as artifacts on the
[CI workflow](https://github.com/nagylzs/tea/actions/workflows/go.yml).

To build from source:

```sh
git clone https://github.com/nagylzs/tea.git
cd tea
go build -o tea ./cmd/tea          # plain build, "tea dev" in --version
python3 scripts/build.py           # static release binaries with version info -> dist/linux/{amd64,arm64}/tea
```

## How it works

```
tea [GLOBAL OPTION...] COMMAND [COMMAND...] -- PROGRAM [ARG...]
```

A **command** is a group of options: conditions that select lines, and actions to perform on them. The first command
starts with the first command level option; further commands start with `-c`, optionally with a name so that other
commands can enable, disable, toggle or jump to it.

For every line, `tea` walks the commands in order. A command applies when it is enabled, the line comes from a stream
it listens to (stdout by default; `--std-err`, `--std-all`) and its patterns match. Then all of its actions run.

| Family | Options |
|---|---|
| Conditions | `-p PATTERN` (Go regexp, repeatable, AND by default), `--or`, `--no`, `--std-err`, `-a` |
| Time based conditions | `--no-input-for DURATION` (quiet period), `-t DURATION` (deadline since last enable) |
| Output | `--set-prefix`, `--set-suffix`, `-m MARK`, `--mark-stderr`, `--send-to-stdout`, `--send-to-stderr` |
| Color | `--fg-color`, `--bg-color`, `--bold`, `--faint`, `--italic`, `--underline`, `--blink` |
| Input | `-i INPUT`, `-f FILE`, `--close` |
| Signals and exit code | `-s SIGNAL`, `-e CODE`, `--clear-exit-code`, `-x CODE` (stop the program and exit with CODE) |
| Control flow | `--enable NAME`, `--disable NAME`, `--toggle NAME`, `-n` (next line), `--skip-to NAME` |
| Global | `--deadline`, `--stop-signal`, `--no-stdin`, `--no-stdbuf`, `--share-commands`, `--share-streams`, `--pid`, `--line-buffer-size` |

Some details worth knowing:

- **Two chains by default.** stdout and stderr are processed in parallel by separate copies of the command chain, so
  enable/disable state is per stream. `--share-commands` uses one chain for both; `--share-streams` merges the
  streams. Time based commands are always a single instance.
- **Stdin is forwarded.** `tea` passes its own stdin to the program and forwards end of file, like running the program
  directly. `-i` and `-f` write into the same ordered queue. Use `--no-stdin` when `tea` runs in the background of an
  interactive shell or when its stdin is already at end of file but you rely on `-i`.
- **Exit code.** By default `tea` exits with the program's exit code. `-e`, `-x` and `--deadline` override it.
- **Buffering.** `tea` can only react to lines the program has flushed. It runs the program through `stdbuf -oL` so
  glibc programs line-buffer, but Python, Perl and Ruby need their own switch (`python3 -u`, `$| = 1`,
  `$stdout.sync = true`). The help text has a section on this per runtime.

The complete reference is `tea --help`, also readable as [`cmd/tea/USAGE.txt`](cmd/tea/USAGE.txt).

## Examples

Reload nginx once no request has arrived for ten seconds, printing a dot per request while waiting:

```sh
(tea -m . -c --no-input-for 10s --exit 0 -- tail -f /var/log/nginx/access.log) && killall -HUP nginx
```

Wait for a PostgreSQL container, written out without the `--exit` and `--deadline` shorthands and interrupting
`docker logs` with SIGINT instead of SIGTERM:

```sh
docker start pg && tea \
    -c -p "ready to accept connections" --set-exit-code 0 --signal SIGINT \
    -c --timeout 120s --set-exit-code 1 --signal SIGINT \
    -- docker logs -f pg
```

Drive a build: fail fast on the first error line, otherwise keep the build's own exit code:

```sh
tea --std-err -p "^ERROR" --exit 1 -- make all
```

## Development

```sh
go vet ./...
go test ./...                       # parser tests plus end-to-end tests; needs python3 and stdbuf
go test ./test/ -run TestTimeout -v # one end-to-end group
```

Layout:

- `cmd/tea/` — the runtime: process start, line readers, the two command chains, the timer for time based commands,
  the stdin writer. `USAGE.txt` is embedded and printed by `--help`; it is the option reference, keep it in sync.
- `internal/opts/` — hand-written argument parser and validator.
- `test/` — end-to-end tests that build the binary and drive it against small Python programs.

`TODO.md` lists open items. `--or-timeout` and `--min-match-time` are parsed but not implemented.

## License

[Apache License 2.0](LICENSE)
