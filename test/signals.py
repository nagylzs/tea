#!/usr/bin/env python3
"""Child process that reports received signals, for tea's end-to-end tests.

Prints "ready", then waits. For every SIGUSR1, SIGUSR2, SIGINT or SIGHUP it
prints "got <NAME>". SIGTERM prints "got SIGTERM" and exits with 0.
If nothing terminates it within 10 seconds it prints "timeout" and exits 99.
"""
import signal
import sys
import time


def handler(signum, frame):
    name = signal.Signals(signum).name
    sys.stdout.write(f"got {name}\n")
    sys.stdout.flush()
    if signum == signal.SIGTERM:
        sys.exit(0)


for sig in (signal.SIGUSR1, signal.SIGUSR2, signal.SIGINT, signal.SIGHUP, signal.SIGTERM):
    signal.signal(sig, handler)

sys.stdout.write("ready\n")
sys.stdout.flush()
for _ in range(100):
    time.sleep(0.1)
sys.stdout.write("timeout\n")
sys.stdout.flush()
sys.exit(99)
