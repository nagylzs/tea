#!/usr/bin/env python3
"""Run a command with its stdout and stderr attached to a pseudo-terminal.

tea's color output (fatih/color) is disabled automatically when stdout is not
a TTY, so the color tests run tea through this wrapper. Everything the command
writes to the pty is copied to our stdout; our exit code is the command's.
"""
import os
import pty
import subprocess
import sys


def main() -> int:
    master, slave = pty.openpty()
    env = dict(os.environ, TERM="xterm")
    env.pop("NO_COLOR", None)
    proc = subprocess.Popen(
        sys.argv[1:], stdin=subprocess.DEVNULL, stdout=slave, stderr=slave, env=env
    )
    os.close(slave)
    chunks = []
    while True:
        try:
            data = os.read(master, 65536)
        except OSError:  # EIO: the slave side was closed
            break
        if not data:
            break
        chunks.append(data)
    os.close(master)
    sys.stdout.buffer.write(b"".join(chunks))
    sys.stdout.flush()
    return proc.wait()


if __name__ == "__main__":
    sys.exit(main())
