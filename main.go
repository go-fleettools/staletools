// staletools reports which installed Go tools were built with an older
// toolchain — and, of those, which actually PARSE GO SOURCE, because only
// for those is staleness a wrong answer rather than an old binary.
//
// # THE DISTINCTION, WHICH IS THE WHOLE TOOL
//
// A CLI built with go1.26 runs perfectly well under a go1.27 toolchain. It
// is old, not broken, and telling somebody to rebuild it is noise.
//
// A tool that analyses Go source embeds go/types, go/parser and friends
// FROM ITS BUILD TOOLCHAIN. Built with go1.26 it cannot read go1.27, and it
// does not fail cleanly — it emits a page of type errors about the standard
// library and then reports on whatever it managed to load:
//
//	rand.go:213:17: method must have no type parameters
//	-: This application uses version go1.26 of the source-processing
//	   packages but runs version go1.27 of 'go list'.
//
// Measured 2026-10-08: `deadcode` did exactly that across go-pkgx, and the
// output reads like findings. After a rebuild it reported 0 — a figure that
// means something only because a deliberately unreachable function was
// added first to prove the tool could still see one.
//
// # IT DOES NOT GUESS WHICH TOOLS THOSE ARE
//
// `go version -m` already prints the toolchain and the module graph. What
// it does not do is say which entries matter. This reads the same build
// info and looks for the source-processing packages in the DEPENDENCY LIST
// — go/types is in the standard library, so the signal is the x/tools and
// analysis modules a source reader must import. That is evidence out of the
// binary, not a list of names somebody maintained.
//
// # AND IT FINDS THE DUPLICATE, WHICH IS THE QUIET ONE
//
// A tool installed in two directories — ~/go/bin because `go install` puts
// it there, ~/.local/bin because somebody put it there deliberately — runs
// whichever PATH reaches first. Observed on this machine: every fleet
// scanner present twice, the ~/.local/bin copies current and the ~/go/bin
// copies four months behind. Nothing was wrong until a shell added
// ~/go/bin.
package main

import (
	"debug/buildinfo"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const usage = `staletools — which Go tools are built with an older toolchain, and which of those read Go source

usage:
  staletools [-all] [-want go1.27.1] [dir...]

With no directory it looks at $GOBIN or $GOPATH/bin, and ~/.local/bin.

exit status:
  0  no SOURCE-READING tool is behind, and no duplicates
  1  something is behind or shadowed — the message says which
  2  refused: a directory that cannot be read
`

// sourceReaders are the modules a tool must import to parse Go source.
//
// go/types and go/parser are in the STANDARD LIBRARY, so they leave no
// trace in the module list — which is why the signal is these. A tool that
// walks packages uses go/packages; one that runs analysers uses
// go/analysis; both live in x/tools.
var sourceReaders = []string{
	"golang.org/x/tools",
	"honnef.co/go/tools",
	"github.com/golangci/golangci-lint",
	"mvdan.cc/gofumpt",
}

type tool struct {
	name, dir, path, goVersion string
	readsSource                bool
}

func main() {
	fs := flag.NewFlagSet("staletools", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	all := fs.Bool("all", false, "list every Go tool, not only the ones behind")
	want := fs.String("want", runtime.Version(), "the toolchain to compare against")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	dirs := fs.Args()
	if len(dirs) == 0 {
		dirs = defaultDirs()
	}
	tools, err := scan(dirs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "staletools:", err)
		os.Exit(2)
	}
	if report(os.Stdout, tools, *want, *all) {
		os.Exit(1)
	}
}

func defaultDirs() []string {
	var out []string
	if v := os.Getenv("GOBIN"); v != "" {
		out = append(out, v)
	} else if v := os.Getenv("GOPATH"); v != "" {
		out = append(out, filepath.Join(v, "bin"))
	} else if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, "go", "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".local", "bin"))
	}
	return out
}

// scan reads the build info of every regular file in each directory.
//
// A file that is not a Go binary is skipped in silence: these directories
// hold shell scripts and symlinks too, and a tool that complained about
// each one would bury its own answer.
func scan(dirs []string) ([]tool, error) {
	var out []tool
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			// A directory that does not exist is not an error: not every
			// machine has both.
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(dir, e.Name())
			bi, err := buildinfo.ReadFile(p)
			if err != nil {
				continue
			}
			t := tool{name: e.Name(), dir: dir, path: bi.Path, goVersion: bi.GoVersion}
			paths := make([]string, 0, len(bi.Deps))
			for _, d := range bi.Deps {
				paths = append(paths, d.Path)
			}
			t.readsSource = readsGoSource(paths)
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return out[i].dir < out[j].dir
	})
	return out, nil
}

// report prints, and says whether anything needs doing.
func report(w io.Writer, tools []tool, want string, all bool) (problem bool) {
	byName := map[string][]tool{}
	for _, t := range tools {
		byName[t.name] = append(byName[t.name], t)
	}

	var stale, staleSource []tool
	for _, t := range tools {
		if t.goVersion == want {
			continue
		}
		stale = append(stale, t)
		if t.readsSource {
			staleSource = append(staleSource, t)
		}
	}

	// THE ONES THAT MATTER FIRST. A page of old CLIs would bury them.
	if len(staleSource) > 0 {
		problem = true
		fmt.Fprintf(w, "READ GO SOURCE and are behind %s — these do not fail, they MISREPORT:\n", want)
		for _, t := range staleSource {
			fmt.Fprintf(w, "  %-22s %-12s %s\n", t.name, t.goVersion, t.dir)
			fmt.Fprintf(w, "  %-22s go install %s@latest\n", "", t.path)
		}
	}

	var dups []string
	for name, ts := range byName {
		if len(ts) < 2 {
			continue
		}
		same := true
		for _, t := range ts[1:] {
			if t.goVersion != ts[0].goVersion {
				same = false
			}
		}
		if !same {
			dups = append(dups, name)
		}
	}
	sort.Strings(dups)
	if len(dups) > 0 {
		problem = true
		fmt.Fprintf(w, "\nINSTALLED TWICE with different toolchains — PATH order decides which runs:\n")
		for _, name := range dups {
			fmt.Fprintf(w, "  %s\n", name)
			for _, t := range byName[name] {
				fmt.Fprintf(w, "      %-12s %s\n", t.goVersion, t.dir)
			}
		}
	}

	if all {
		fmt.Fprintf(w, "\nevery Go tool found:\n")
		for _, t := range tools {
			mark := " "
			if t.goVersion != want {
				mark = "·"
			}
			src := ""
			if t.readsSource {
				src = "  reads Go source"
			}
			fmt.Fprintf(w, "%s %-22s %-12s %s%s\n", mark, t.name, t.goVersion, t.dir, src)
		}
	}

	// THE TOTALS ALWAYS, including the zeros. A command that prints nothing
	// when all is well is indistinguishable from one that failed to look —
	// and this one walks directories that may not exist.
	fmt.Fprintf(w, "\n%d Go tool(s), %d behind %s, %d of those read Go source\n",
		len(tools), len(stale), want, len(staleSource))
	if len(tools) == 0 {
		fmt.Fprintln(w, "no Go binaries found — is that the right directory?")
	}
	return problem
}

// readsGoSource reports whether a module list belongs to a tool that parses
// Go source.
//
// Extracted so it can be tested without building a binary that imports
// x/tools: the rule is the interesting part, and a test that had to compile
// gopls to check it would never run.
//
// A PREFIX MATCH, with the slash. "golang.org/x/tools" must match
// "golang.org/x/tools/gopls" and must NOT match
// "golang.org/x/toolsomething" — the slash is what separates a submodule
// from a different module whose name starts the same way.
func readsGoSource(deps []string) bool {
	for _, d := range deps {
		for _, m := range sourceReaders {
			if d == m || strings.HasPrefix(d, m+"/") {
				return true
			}
		}
	}
	return false
}
