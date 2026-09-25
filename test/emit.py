#!/usr/bin/env python3
"""Scriptable child process for tea's end-to-end tests.

Each argument is a token that is executed in order:

  out:TEXT    write TEXT and a newline to stdout
  err:TEXT    write TEXT and a newline to stderr
  sleep:SEC   sleep SEC seconds (float)
  exit:CODE   exit immediately with CODE
  pid         write our own pid to stdout
  cat:FILE    write the contents of FILE to stdout (followed by a newline)
  long:N      write a line of N 'x' characters to stdout
  write:PATH=TEXT
              write TEXT and a newline into the file PATH (creating it)
  stdin       read stdin until EOF, writing "input: <line>" for every line,
              then write "eof"
  count       read stdin (binary) until EOF, then write "bytes: <n>"

All writes are flushed immediately, so output buffering never depends on
stdbuf(1) (which python ignores anyway).
"""
import os
import sys
import time


def main() -> int:
    for token in sys.argv[1:]:
        kind, _, arg = token.partition(":")
        if kind == "out":
            sys.stdout.write(arg + "\n")
            sys.stdout.flush()
        elif kind == "err":
            sys.stderr.write(arg + "\n")
            sys.stderr.flush()
        elif kind == "sleep":
            time.sleep(float(arg))
        elif kind == "exit":
            return int(arg)
        elif kind == "pid":
            sys.stdout.write(f"{os.getpid()}\n")
            sys.stdout.flush()
        elif kind == "cat":
            with open(arg) as f:
                sys.stdout.write(f.read() + "\n")
            sys.stdout.flush()
        elif kind == "long":
            sys.stdout.write("x" * int(arg) + "\n")
            sys.stdout.flush()
        elif kind == "write":
            path, _, text = arg.partition("=")
            with open(path, "w") as f:
                f.write(text + "\n")
        elif kind == "stdin":
            for line in sys.stdin:
                sys.stdout.write(f"input: {line.rstrip(chr(10))}\n")
                sys.stdout.flush()
            sys.stdout.write("eof\n")
            sys.stdout.flush()
        elif kind == "count":
            data = sys.stdin.buffer.read()
            sys.stdout.write(f"bytes: {len(data)}\n")
            sys.stdout.flush()
        else:
            sys.stderr.write(f"emit.py: unknown token {token!r}\n")
            return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
