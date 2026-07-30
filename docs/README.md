# docs

## `index.html` — the dependency graph

Published at **[xpra-org.github.io/go-xpra](https://xpra-org.github.io/go-xpra/)**: GitHub Pages
serves this directory, so `index.html` is what that URL resolves to. It is an interactive map of
every package this client compiles, for both targets, and a single self-contained page — no
external stylesheets, scripts or fonts — so a local checkout works just as well offline
(`xdg-open docs/index.html`). Browsing to the file on github.com does not, because GitHub serves
`.html` from a repository as source rather than rendering it.

Packages are the nodes, because packages are what `go build` compiles; modules are what `go.mod`
declares, so they own the wedges, the left rail and the version story. Standard library packages are
hidden by default and switched on at the top of the page — they are two thirds of the graph, and
which of them a dependency drags in is the whole point of showing them.

Three views over the same data, switched above the stage:

- **Graph** — a radial map. Rings are imports deep from the `main` package; each module owns an
  angular wedge, sized by the number of packages *only it* reaches and named on the rim. Cyan means
  third-party, grey the standard library, hollow that we write that import ourselves, and an amber
  halo that the module was resolved past a version something in the graph asked for. Drag to pan,
  scroll to zoom, click for detail.
- **Tree** — the import tree from the `main` package down, with `(*)` marking a subtree already
  expanded above it and the module version printed once per module.
- **Table** — sortable: package, module and version, kind, imports deep, which modules reach it, how
  many packages import it, file count, license.

The left rail lists the modules `go.mod` requires with a bar each: the bar's length is every package
that module reaches, and its solid part is the packages it is the *only* route to — what dropping it
would actually remove. Clicking one narrows every view to its subtree. Our own module leads the
list; what is exclusive to it is the standard library nothing else needs. A module `go.mod` requires
that the selected target compiles nothing of is listed but not clickable, and below the list are the
modules that are resolved and checksummed in `go.sum` without a line of them ever being built.

### Regenerating

After any dependency change:

```shell
python3 docs/dependency-graph.py
```

That rewrites `docs/index.html` from `docs/dependency-graph.template.html`, inlining a freshly read
graph. It needs nothing but python 3 and a working `go` — no third-party python
packages, and no cross toolchain for the Windows target, since `go list` only has to *resolve* the
graph, not build it. It takes well under a second.

The data comes from `go list -deps -json`, run once per target with that target's `GOOS`/`GOARCH`
and `CGO_ENABLED=0`, matching what `release.yml` ships. That applies build constraints, so each
target's graph is exactly what `go build` compiles for it — x11 and wayland on Linux, win32 on
Windows — rather than the union of everything the module could reach. `go list -m -json all` then
supplies module versions and directness, `go mod graph` the requirement edges behind the version
bumps, and each module's own `LICENSE` file its SPDX id, sniffed from the licence text.

Ownership is computed on the package graph: for each module `go.mod` requires directly, every
package reachable from any package of it. A package with one owner is one that dropping that module
would take with it. Packages of our own module — and any standard library package no dependency
reaches — belong to the main module's own wedge instead.

Adding a target means adding it to `TARGETS` in the script *and* adding a matching
`<button data-target=…>` to the template's target switch.
