import os
import sys
import re
import platform
import copy
import datetime
import subprocess as sp

targets=["tea"]

DIR=os.path.abspath(os.path.join(__file__,os.pardir,os.pardir))

module_line = next(open(os.path.join(DIR, "go.mod"), "r"))
module_name = re.match(r"module\s+(.+)", module_line).group(1)
version_prefix=f"{module_name}/internal/version"


oss = platform.system().lower()
if oss not in ["windows", "linux"]:
    raise SystemExit(f"unknown system {oss}")

is_mingw = False
try:
    import sysconfig
    is_mingw = "mingw" in sysconfig.get_platform().lower()
except ImportError:
    pass

arch = platform.machine().lower()
if arch=='x86_64':
    arch = "amd64"
if arch not in ["amd64", "i386"]:
    raise SystemExit(f"unknown arch {arch}")

print(f"os={oss} arch={arch}")

def co(cmd):
    o = sp.check_output(cmd)
    return o.decode("utf-8").strip()

def run(cmd):
    print(cmd[0])
    for item in cmd[1:]:
        print("",item)
    ret = sp.call(cmd)
    if ret != 0:
        raise SystemExit(ret)

built=datetime.datetime.now().isoformat()
branch = co(["git", "rev-parse", "--abbrev-ref", "HEAD"])
commit = co(["git", "rev-parse", "HEAD"])

print(f"built={built} branch={branch} commit={commit}")

ext = ""
ldflags=[
    "-X", f"{version_prefix}.Built={built}",
    "-X", f"{version_prefix}.Commit={commit}",
    "-X", f"{version_prefix}.Branch={branch}",
]
ldflags_debug = copy.copy(ldflags)
ldflags += ["-s", "-w"]
ldflags_gui = copy.copy(ldflags)
if oss=="windows" and not is_mingw:
    ldflags += ["-tags", "timetzdata"]
    ldflags_gui += ["-H=windowsgui"]
    ext = ".exe"

os.environ["GOOS"] = oss
os.environ["GOARCH"] = arch
ddir = os.path.join(DIR, "dist", oss, arch)
if not os.path.isdir(ddir):
    os.makedirs(ddir)

os.chdir(DIR)
print("go mod tidy")
run(["go", "mod", "tidy"])
for target in targets:
    targetdir = os.path.join(DIR, "cmd", target)
    os.chdir(targetdir)
    src = os.path.join(targetdir, target+".go")
    if not os.path.isfile(src):
        print(f"source file {src} not found")
        raise SystemExit(1)
    binary = os.path.join(ddir, target + ext)
    winres = os.path.join(targetdir, "winres", "winres.json")
    is_gui = os.path.isfile(winres)
    if is_gui:
        sldflags = " ".join(ldflags_gui)
    else:
        sldflags = " ".join(ldflags)
    cmd = ["go", "build", "-ldflags", sldflags, "-o", binary, src]
    print(" ".join(cmd))
    run(cmd)
    if is_gui and oss=="windows":
        run(["go-winres", "patch", "--no-backup", binary])


    binary = os.path.join(ddir, target + "_debug" + ext)
    sldflags = " ".join(ldflags_debug)
    cmd = ["go", "build", "-ldflags", sldflags, "-o", binary, src]
    print(" ".join(cmd))
    run(cmd)
    if is_gui and oss=="windows":
        run(["go-winres", "patch", "--no-backup", binary])


