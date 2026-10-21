#!/usr/bin/env python3
"""Release build for tea (Linux and macOS).

usage: scripts/build.py [--debug] [OS/ARCH...]

Builds dist/OS/ARCH/tea for every target given (default: linux/amd64
linux/arm64 darwin/amd64 darwin/arm64; a bare ARCH means linux/ARCH) as a
stripped binary with -trimpath and cgo disabled, which makes the Linux binaries
static, and injects the version, commit, branch and build time into
internal/version. The branch is empty on a detached HEAD (CI checks out the
tag of a release build; the version names the tag then). --debug keeps
symbols and DWARF.
The working tree is not modified.
"""
import datetime
import os
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
DEFAULT_TARGETS = ["linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"]


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
    targets = [a if "/" in a else f"linux/{a}" for a in argv if not a.startswith("--")] or DEFAULT_TARGETS

    version_pkg = f"{module_name()}/internal/version"
    version = git("describe", "--tags", "--match", "v*", "--always", "--dirty")
    commit = git("rev-parse", "HEAD")
    branch = git("rev-parse", "--abbrev-ref", "HEAD")
    if branch == "HEAD":
        branch = ""  # detached, see the docstring
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

    for target in targets:
        goos, arch = target.split("/")
        out = ROOT / "dist" / goos / arch / "tea"
        out.parent.mkdir(parents=True, exist_ok=True)
        env = dict(os.environ, GOOS=goos, GOARCH=arch, CGO_ENABLED="0")
        cmd = ["go", "build", "-trimpath", "-ldflags", " ".join(ldflags), "-o", str(out), "./cmd/tea"]
        print(" ".join(cmd))
        subprocess.check_call(cmd, cwd=ROOT, env=env)
        print(f"-> {out.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
