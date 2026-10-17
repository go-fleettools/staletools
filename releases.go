package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// onPath reports whether a directory is one the shell actually searches.
//
// ⛔ Without this the report leads with noise. Measured on 2026-10-17: of 30
// lines, 24 named binaries in ~/go/bin, which is NOT on this machine's PATH —
// `tui-explorer` 65 releases behind in a directory nothing reads, printed above
// `pkgx` 2 behind in the directory that runs. An install that lands outside PATH
// looks exactly like an install that worked, and so does a stale one.
func onPath(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == "" {
			continue
		}
		if q, err := filepath.Abs(p); err == nil && q == abs {
			return true
		}
	}
	return false
}

// ⛔ The second axis, and it is not the first one.
//
// `-want` asks whether a tool was BUILT with an older toolchain, which makes an
// analyser misreport. This asks whether the tool is an older RELEASE of itself,
// which makes it do the wrong thing entirely.
//
// The founding case: on 2026-10-11 the `gitpush` on PATH was v0.5.0 while its
// repository was at v0.6.3, and the global pre-push hook was built from v0.3.2.
// Nothing looked wrong. "The hook is the floor underneath" reads as a statement
// about the current code, and it was a week behind. `go version -m` on each
// binary would have said so, and nobody runs that on sixty binaries by hand.
//
// It is behind a FLAG because it needs the network: the default run stays local
// and immediate, which is what makes it something one actually runs.

// release is what the module proxy says about one module.
type release struct {
	Mod      string
	Latest   string   // the newest released version, "" when there is none
	Versions []string // every released version, oldest first, as the go command orders them
	Err      error
}

// has reports the position of v in the ordered list, or -1.
func (r release) has(v string) int {
	for i, got := range r.Versions {
		if got == v {
			return i
		}
	}
	return -1
}

// askProxy asks the module proxy for every released version of mod.
//
// ⭐ `go list -m -versions` is the oracle rather than a tag listing or the GitHub
// API, for three reasons: it answers the question actually being asked — what
// `@latest` would install — it orders the versions ITSELF, semantically, so this
// file needs no version parser and no dependency, and it costs no GitHub API
// budget, which on this machine is shared with Renovate and is exhaustible.
//
// ⛔ It is NOT a string comparison against the newest. A tool built from an
// unreleased commit carries a pseudo-version that appears nowhere in the list,
// and calling that "behind" without saying why would be the same mislabel this
// file's sibling `behind` was fixed for.
func askProxy(mod string) release {
	r := release{Mod: mod}
	out, err := exec.Command("go", "list", "-m", "-versions", mod).Output()
	if err != nil {
		r.Err = fmt.Errorf("cannot ask the proxy about %s: %w", mod, err)
		return r
	}
	f := strings.Fields(strings.TrimSpace(string(out)))
	if len(f) > 1 {
		r.Versions = f[1:]
		r.Latest = r.Versions[len(r.Versions)-1]
	}
	return r
}

// verdict is what one installed tool's version means.
type verdict int

const (
	current    verdict = iota // the newest release
	behindRel                 // an older release
	unreleased                // built from a commit that is not a release
	unknown                   // the proxy could not be asked
)

func judge(installed string, r release) verdict {
	switch {
	case r.Err != nil:
		return unknown
	case r.Latest == "":
		return unknown
	case installed == r.Latest:
		return current
	case r.has(installed) >= 0:
		return behindRel
	default:
		return unreleased
	}
}

// reportReleases says which installed tools are an older release of themselves.
// It returns true when something is.
func reportReleases(w io.Writer, tools []tool) bool {
	mods := map[string]bool{}
	for _, t := range tools {
		if t.mod != "" && t.mod != "command-line-arguments" {
			mods[t.mod] = true
		}
	}
	names := make([]string, 0, len(mods))
	for m := range mods {
		names = append(names, m)
	}
	sort.Strings(names)

	asked := map[string]release{}
	for _, m := range names {
		asked[m] = askProxy(m)
	}

	type row struct {
		t      tool
		r      release
		v      verdict
		behind int  // how many releases back
		live   bool // in a directory the shell searches
	}
	var old, loose, mute []row
	for _, t := range tools {
		r, ok := asked[t.mod]
		if !ok {
			continue
		}
		v := judge(t.modVersion, r)
		rw := row{t: t, r: r, v: v, live: onPath(t.dir)}
		switch v {
		case behindRel:
			rw.behind = len(r.Versions) - 1 - r.has(t.modVersion)
			old = append(old, rw)
		case unreleased:
			loose = append(loose, rw)
		case unknown:
			mute = append(mute, rw)
		}
	}
	// ⛔ On PATH first, whatever the distance. One release behind in a directory
	// that RUNS beats sixty-five behind in one nothing reads.
	sort.Slice(old, func(i, j int) bool {
		if old[i].live != old[j].live {
			return old[i].live
		}
		if old[i].behind != old[j].behind {
			return old[i].behind > old[j].behind
		}
		return old[i].t.name < old[j].t.name
	})

	if len(old) > 0 {
		live := 0
		for _, rw := range old {
			if rw.live {
				live++
			}
		}
		fmt.Fprintf(w, "\nan OLDER RELEASE of themselves — what runs is not what the repository says.\n")
		fmt.Fprintf(w, "→ marks the %d in a directory the shell SEARCHES; the other %d are in one nothing reads:\n",
			live, len(old)-live)
		for _, rw := range old {
			mark := "  "
			if rw.live {
				mark = "→ "
			}
			fmt.Fprintf(w, "%s%-22s %-14s → %-14s %d release(s) back   %s\n",
				mark, rw.t.name, rw.t.modVersion, rw.r.Latest, rw.behind, rw.t.dir)
			fmt.Fprintf(w, "  %-22s go install %s@%s\n", "", rw.t.path, rw.r.Latest)
		}
	}
	if len(loose) > 0 {
		fmt.Fprintf(w, "\nbuilt from a commit that is NOT a release — reproducible only if that commit is known:\n")
		for _, rw := range loose {
			fmt.Fprintf(w, "  %-22s %-30s latest release %s   %s\n",
				rw.t.name, rw.t.modVersion, rw.r.Latest, rw.t.dir)
		}
	}
	// ⛔ Separately, and always: a module the proxy could not answer about is not
	// a module that is up to date. A scan that could not read must not be
	// reported as a clean machine.
	if len(mute) > 0 {
		fmt.Fprintf(w, "\nthe proxy could not be asked about these — NOT a verdict of up to date:\n")
		for _, rw := range mute {
			why := "no released version"
			if rw.r.Err != nil {
				why = rw.r.Err.Error()
			}
			fmt.Fprintf(w, "  %-22s %-30s %s\n", rw.t.name, rw.t.modVersion, why)
		}
	}
	liveOld := 0
	for _, rw := range old {
		if rw.live {
			liveOld++
		}
	}
	fmt.Fprintf(w, "\n%d module(s) asked: %d tool(s) on an older release (%d of them on PATH), "+
		"%d from an unreleased commit, %d unanswerable\n",
		len(names), len(old), liveOld, len(loose), len(mute))
	return len(old) > 0
}
