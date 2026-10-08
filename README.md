# staletools

Which installed Go tools were built with an older toolchain — and, of those, **which
actually parse Go source**, because only for those is staleness a *wrong answer* rather
than an old binary.

```console
$ staletools
READ GO SOURCE and are behind go1.27.1 — these do not fail, they MISREPORT:
  gopls                  go1.26.4     ~/go/bin
                         go install golang.org/x/tools/gopls@latest
  nonnil                 go1.26.4     ~/go/bin
                         go install github.com/go-vet-analyzers/nonnil/cmd/nonnil@latest
  …

INSTALLED TWICE with different toolchains — PATH order decides which runs:
  agentsync
      go1.27.1     ~/.local/bin
      go1.26.4     ~/go/bin
  …

81 Go tool(s), 29 behind go1.27.1, 8 of those read Go source
```

| exit | |
|---|---|
| `0` | no source-reading tool is behind, and nothing is shadowed |
| `1` | something is behind or shadowed — the message says which |
| `2` | refused: a directory that cannot be read |

## Why the distinction is the whole tool

A CLI built with go1.26 runs perfectly well under a go1.27 toolchain. It is **old, not
broken**, and telling somebody to rebuild it is noise.

A tool that analyses Go source embeds `go/types`, `go/parser` and friends **from its build
toolchain**. Built with go1.26 it cannot read go1.27 — and it does not fail cleanly. It
emits a page of type errors about the *standard library* and then reports on whatever it
managed to load:

```
rand.go:213:17: method must have no type parameters
-: This application uses version go1.26 of the source-processing packages
   but runs version go1.27 of 'go list'.
```

Measured 2026-10-08: `deadcode` did exactly that across a five-module project, and the
output reads like findings. After a rebuild it reported **0** — a figure that means
something only because a deliberately unreachable function was added first, to prove the
tool could still see one.

## It does not guess which tools those are

[`go version -m`](https://pkg.go.dev/cmd/go#hdr-Print_Go_version) already prints the
toolchain and the module graph; what it does not do is say **which entries matter**.

This reads the same build info and looks for the source-processing modules in the
**dependency list**. `go/types` is in the standard library and leaves no trace, so the
signal is `golang.org/x/tools`, `honnef.co/go/tools` and the like — evidence out of the
binary, not a list of names somebody has to maintain.

The prefix match keeps the slash: `golang.org/x/tools` matches `golang.org/x/tools/gopls`
and **not** `golang.org/x/toolsomething`.

## And it finds the duplicate, which is the quiet one

A tool installed in two directories — `~/go/bin` because `go install` puts it there,
`~/.local/bin` because somebody put it there deliberately — runs whichever `PATH` reaches
first.

Observed on the machine this was written for: **six** tools present twice, the
`~/.local/bin` copies current and the `~/go/bin` copies four months behind. Nothing was
wrong until a shell added `~/go/bin`, and then the answer would change without the command
changing.

Two copies of the **same** build are not reported: that is housekeeping, and flagging it
would train people to skip the section.

## Install

```
go install github.com/go-fleettools/staletools@latest
```

No dependencies: `debug/buildinfo` is in the standard library.

## Usage

```
staletools [-all] [-want go1.27.1] [dir...]
```

With no directory it looks at `$GOBIN` (or `$GOPATH/bin`, or `~/go/bin`) and
`~/.local/bin`. A directory that does not exist is skipped — not every machine has both.

`-want` compares against something other than the running toolchain, for asking *"what
would be stale if we moved to this?"* before moving.

## The totals are always printed, including the zeros

A command that says nothing when all is well cannot be told apart from one that failed to
look — and this one walks directories that may not exist. If it finds no Go binaries at
all it says so and asks whether that was the right directory.
