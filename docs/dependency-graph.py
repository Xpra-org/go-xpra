#!/usr/bin/env python3
"""Regenerate `docs/dependency-graph.html`, the interactive dependency graph.

    python3 docs/dependency-graph.py [main-package]

Reads the package graph straight out of `go list` and inlines it into
`dependency-graph.template.html`, producing one self-contained page (no network
access at view time, no external assets).

Packages, not modules, are the nodes, because packages are what the toolchain
actually compiles: `go list -deps` applies build constraints, so the graph is
exactly what `go build` puts in the binary for that GOOS/GOARCH - the x11 and
wayland backends on Linux, win32 on Windows - while a module contributes as many
or as few packages as its files' constraints allow. Modules are still what
`go.mod` declares, so they own the wedges, the left rail and the version story:
`go list -m all` reports every module in the graph, including the ones pinned in
`go.sum` that no build compiles a line of.

Adding a target: put it in TARGETS. The keys become the page's target switch, so
the template's `<button data-target=...>` list has to match.
"""

import collections
import datetime
import json
import os
import pathlib
import re
import subprocess
import sys

DOCS = pathlib.Path(__file__).resolve().parent
ROOT = DOCS.parent

# GOOS/GOARCH per target, matching what release.yml ships - cgo is off there
# too, so this is the graph that actually gets built.
TARGETS = {
    "linux": ("linux", "amd64"),
    "windows": ("windows", "amd64"),
}

# everything the page needs out of `go list`, and nothing else: the unfiltered
# -json output for ~250 packages is an order of magnitude more text
FIELDS = "ImportPath,Doc,Standard,Module,Imports,GoFiles,CgoFiles"

# the standard library has no module of its own; giving it one means every node
# can name the module it came from
STD = "std"

LICENSE_FILES = ("LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "LICENCE.txt",
                 "COPYING", "COPYING.txt", "LICENSE-MIT", "LICENSE-BSD")

# first match wins, so the narrower patterns come first
LICENSES = (
    (r"Apache License.{0,40}Version 2\.0", "Apache-2.0"),
    (r"Mozilla Public License.{0,40}Version 2\.0", "MPL-2.0"),
    (r"GNU LESSER GENERAL PUBLIC LICENSE.{0,60}Version 3", "LGPL-3.0"),
    (r"GNU GENERAL PUBLIC LICENSE.{0,60}Version 3", "GPL-3.0"),
    (r"GNU GENERAL PUBLIC LICENSE.{0,60}Version 2", "GPL-2.0"),
    (r"Permission to use, copy, modify, and distribute this software for any", "ISC"),
    (r"Permission is hereby granted, free of charge", "MIT"),
    (r"Redistribution and use in source and binary forms.{0,3000}Neither the name", "BSD-3-Clause"),
    (r"Redistribution and use in source and binary forms", "BSD-2-Clause"),
)

FORGES = ("github.com", "gitlab.com", "codeberg.org", "bitbucket.org", "gitea.com")


def go(*args, **env):
    out = subprocess.run(("go",) + args, cwd=ROOT, capture_output=True, text=True,
                         env=dict(os.environ, **env))
    if out.returncode:
        sys.exit(f"go {' '.join(args)} failed:\n{out.stderr}")
    return out.stdout


def objects(text):
    """`go list -json` emits a stream of objects, not a JSON array."""
    decoder, at, out = json.JSONDecoder(), 0, []
    while True:
        while at < len(text) and text[at].isspace():
            at += 1
        if at >= len(text):
            return out
        obj, at = decoder.raw_decode(text, at)
        out.append(obj)


def license_of(directory):
    """Best-effort SPDX id, sniffed from the module's own licence file."""
    if not directory:
        return ""
    for name in LICENSE_FILES:
        path = pathlib.Path(directory) / name
        if not path.is_file():
            continue
        text = path.read_text(errors="replace")[:8000]
        for pattern, spdx in LICENSES:
            if re.search(pattern, text, re.S | re.I):
                return spdx
        return "see " + name
    return ""


def repo_of(module):
    m = re.match(r"(%s)/([^/]+)/([^/]+)" % "|".join(map(re.escape, FORGES)), module)
    return "https://%s/%s/%s" % m.groups() if m else ""


def short(path, module):
    """A package's label: what is left of it once its module has been named."""
    if module == STD or not module:
        return path                                   # net/http, not http
    if path == module:
        return re.sub(r"/v\d+$", "", module).rpartition("/")[2]
    if path.startswith(module + "/"):
        return path[len(module) + 1:]
    return path


def modules():
    """Every module in the graph, built or not, keyed by path."""
    out = {}
    for m in objects(go("list", "-m", "-json", "all")):
        path = m["Path"]
        out[path] = {
            "v": m.get("Version", ""),
            "lic": license_of(m.get("Dir", "")),
            "repo": repo_of(path),
            # not `// indirect` in go.mod, i.e. something of ours imports it
            "direct": not m.get("Indirect", False),
            "main": m.get("Main", False),
        }
    return out


def bumps(mods):
    """Modules some requirement asks for older than the version MVS picked.

    Go has no duplicate versions - one module path is one version in the build -
    so this is the nearest equivalent: who is being dragged forward, and by what.
    """
    asked = collections.defaultdict(set)
    for line in go("mod", "graph").splitlines():
        by, _, want = line.partition(" ")
        path, _, version = want.partition("@")
        if version and path in mods:
            asked[path].add((by.partition("@")[0], version))

    return {
        path: {"sel": mods[path]["v"],
               "req": sorted([by, v] for by, v in requests if v != mods[path]["v"])}
        for path, requests in asked.items()
        if len({v for _, v in requests}) > 1
    }


def build(goos, goarch, main, mods, bumped):
    pkgs = objects(go("list", "-deps", "-json=" + FIELDS, main,
                      GOOS=goos, GOARCH=goarch, CGO_ENABLED="0"))
    mainmod = next(m for m, v in mods.items() if v["main"])
    by_path = {p["ImportPath"]: p for p in pkgs}
    home = {path: STD if p.get("Standard") else p.get("Module", {}).get("Path", "")
            for path, p in by_path.items()}

    # module -> the packages of it this target compiles, every module keyed even
    # when the answer is none of them
    owned = {m: [] for m in set(home.values()) | set(mods)}
    for path, module in home.items():
        owned[module].append(path)

    # "C" is cgo's pseudo-package, not something go list ever describes
    imports = {path: {imp for imp in p.get("Imports", ())
                      if imp != "C" and imp in home}
               for path, p in by_path.items()}

    # every package each direct dependency can reach, its own included
    wedged = sorted(m for m, v in mods.items()
                    if v["direct"] and not v["main"] and owned[m])
    owners = collections.defaultdict(set)
    for m in wedged:
        seen, stack = set(owned[m]), list(owned[m])
        while stack:
            for nxt in imports[stack.pop()]:
                if nxt not in seen:
                    seen.add(nxt)
                    stack.append(nxt)
        for path in seen:
            owners[path].add(m)

    # ...leaving our own packages, and the standard library packages that no
    # dependency reaches, to the main module's own wedge
    for path, module in home.items():
        if path != main and (module == mainmod or not owners[path]):
            owners[path].add(mainmod)

    # the imports we wrote out ourselves, as against what those drag in behind
    named = {imp for path in owned[mainmod]
             for imp in imports[path] if home[imp] != mainmod}

    # unused-on-this-target modules stay in the rail, without a wedge
    direct = [mainmod] + sorted(m for m, v in mods.items()
                                if v["direct"] and not v["main"])
    wedge = {m: i for i, m in enumerate(direct)}
    keys = sorted(home)
    at = {path: i for i, path in enumerate(keys)}

    # `nodes` is parallel to `keys`, and edges and owners are indices into them:
    # spelling every import path out again costs more than the rest put together
    nodes = []
    for path in keys:
        p = by_path[path]
        nodes.append({
            "n": short(path, home[path]),
            "m": home[path],
            "std": home[path] == STD,
            "own": home[path] == mainmod,
            "named": path in named,
            "files": len(p.get("GoFiles", ())) + len(p.get("CgoFiles", ())),
            "doc": p.get("Doc", "")[:150],
            "owners": sorted(wedge[m] for m in owners[path]),
        })

    return {
        "root": at[main],
        "mainmod": mainmod,
        "direct": direct,
        "keys": keys,
        "nodes": nodes,
        # [importer, imported, 1 if the imported package is standard library]
        "edges": sorted([at[a], at[b], int(home[b] == STD)]
                        for a, to in imports.items() for b in to),
        "mods": {m: dict(v, pkgs=len(owned[m]), bump=bumped.get(m))
                 for m, v in sorted(mods.items())},
        # required, resolved and checksummed in go.sum - and never compiled
        "unbuilt": sorted(m for m, v in mods.items() if not v["main"] and not owned[m]),
        "stats": {
            "pkgs": len(pkgs),
            "third": sum(1 for n in nodes if not n["std"] and not n["own"]),
            "std": len(owned[STD]),
            "own": len(owned[mainmod]),
            "mods": sum(1 for m, p in owned.items() if p and m not in (STD, mainmod)),
        },
    }


def main():
    mains = go("list", "-f", '{{if eq .Name "main"}}{{.ImportPath}}{{end}}', "./...").split()
    # internal/ commands are test helpers, not something anybody ships
    shipped = [m for m in mains if "/internal/" not in m]
    if len(sys.argv) > 1:
        entry = go("list", "-f", "{{.ImportPath}}", sys.argv[1]).strip()
    elif len(shipped) == 1:
        entry = shipped[0]
    else:
        sys.exit("%d main packages (%s): name the one to graph"
                 % (len(shipped), ", ".join(shipped or mains)))

    mods = modules()
    if not any(v["main"] for v in mods.values()):
        sys.exit("no main module - run this from inside the module")
    bumped = bumps(mods)

    mods[STD] = {"v": go("env", "GOVERSION").strip(), "lic": "BSD-3-Clause",
                 "repo": "https://github.com/golang/go", "direct": False, "main": False}

    data = {label: build(goos, goarch, entry, mods, bumped)
            for label, (goos, goarch) in TARGETS.items()}

    blob = json.dumps(data, separators=(",", ":"), sort_keys=True)
    if "</script" in blob:
        sys.exit("refusing to inline data that would close the <script> element")

    head = subprocess.run(("git", "rev-parse", "--short", "HEAD"), cwd=ROOT,
                          capture_output=True, text=True)
    stamp = f"{datetime.date.today():%-d %b %Y}"
    if not head.returncode:
        stamp = f"{head.stdout.strip()}, {stamp}"

    page = (DOCS / "dependency-graph.template.html").read_text()
    for placeholder, value in (("__DATA__", blob), ("__STAMP__", stamp)):
        if placeholder not in page:
            sys.exit(f"template is missing {placeholder}")
        page = page.replace(placeholder, value, 1)

    target_file = DOCS / "dependency-graph.html"
    target_file.write_text(page)

    print(f"wrote {target_file.relative_to(ROOT)} ({len(page) // 1024} KiB)")
    for label, g in data.items():
        s = g["stats"]
        print(f"  {label:8s} {s['pkgs']:4d} packages  {s['third']:3d} third-party  "
              f"{s['std']:4d} stdlib  {s['own']:3d} ours  {s['mods']:2d} modules")
        for m in g["unbuilt"]:
            print(f"             never built: {m}")
    for m, b in sorted(bumped.items()):
        asked = ", ".join(f"{by.rpartition('/')[2]} wants {v}" for by, v in b["req"])
        print(f"  bumped: {m} {b['sel']} ({asked})")


if __name__ == "__main__":
    main()
