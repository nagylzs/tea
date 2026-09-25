#!/usr/bin/env python3
"""Release build for tea (Linux only).

usage: scripts/build.py [--debug] [ARCH...]

Builds dist/linux/ARCH/tea for every ARCH given (default: amd64 arm64) as a
static, stripped binary with -trimpath, and injects the version, commit,
branch and build time into internal/version. --debug keeps symbols and DWARF.
The working tree is not modified.
"""
import datetime
import os
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
DEFAULT_ARCHES = ["amd64", "arm64"]


def module_name() -> str:
    for line in (ROOT / "go.mod").read_text().splitlines():
        if line.startswith("module "):
            return line.split()[1]
    raise SystemExit("go.mod: no module line")


def git(*args: str) -> str:
    try:
        return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()
    except (OSError, subprocess.CalledProcessError) as e:
        raise SystemExit(f"git {' '.join(args)}: {e}")


def main(argv: list[str]) -> int:
    debug = "--debug" in argv
    arches = [a for a in argv if not a.startswith("--")] or DEFAULT_ARCHES

    version_pkg = f"{module_name()}/internal/version"
    version = git("describe", "--tags", "--match", "v*", "--always", "--dirty")
    commit = git("rev-parse", "HEAD")
    branch = git("rev-parse", "--abbrev-ref", "HEAD")
    built = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    print(f"version={version} commit={commit} branch={branch} built={built}")

    ldflags = [
        f"-X {version_pkg}.Version={version}",
        f"-X {version_pkg}.Commit={commit}",
        f"-X {version_pkg}.Branch={branch}",
        f"-X {version_pkg}.Built={built}",
    ]
    if not debug:
        ldflags += ["-s", "-w"]

    for arch in arches:
        out = ROOT / "dist" / "linux" / arch / "tea"
        out.parent.mkdir(parents=True, exist_ok=True)
        env = dict(os.environ, GOOS="linux", GOARCH=arch, CGO_ENABLED="0")
        cmd = ["go", "build", "-trimpath", "-ldflags", " ".join(ldflags), "-o", str(out), "./cmd/tea"]
        print(" ".join(cmd))
        subprocess.check_call(cmd, cwd=ROOT, env=env)
        print(f"-> {out.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
