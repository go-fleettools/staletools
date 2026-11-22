package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ⛔ The authority for gitHookNames is githooks(5), not this repository's
// memory of it. The first draft of that list was written from memory and
// carried `proc-receive-pre-remote`, a name git has never had — one fabricated
// entry in a list whose whole claim is "this is GIT'S list".
//
// So the list is re-read here from the git that is installed, in both
// directions: a name git documents and this list lacks would silently demote a
// live hook, and a name this list invents would print an arrow nothing runs.
//
// ⚠ It SKIPS where the page cannot be read — a container with git but no man
// pages is the normal CI shape — and says which, because a test that passes
// when it could not look is the defect this fleet keeps paying for. The
// invariants that need no page are asserted unconditionally below.
func TestTheHookNameListIsGitsOwnAndNotMine(t *testing.T) {
	page, err := manPage("5", "githooks")
	if err != nil {
		t.Skipf("githooks(5) could not be read, so this list was NOT verified here: %v", err)
	}
	documented := hookHeadings(page)
	if len(documented) < 20 {
		t.Fatalf("githooks(5) yielded only %d hook names (%v) — the page was read but not understood, "+
			"which must not pass as agreement", len(documented), documented)
	}

	var missing, invented []string
	for _, name := range documented {
		if !gitHookNames[name] {
			missing = append(missing, name)
		}
	}
	for name := range gitHookNames {
		if !contains(documented, name) {
			invented = append(invented, name)
		}
	}
	sort.Strings(invented)
	if len(missing) > 0 {
		t.Errorf("githooks(5) documents %d hook(s) this list lacks: %v\n"+
			"each one is a live hook that would be ranked as inert", len(missing), missing)
	}
	if len(invented) > 0 {
		t.Errorf("this list carries %d name(s) githooks(5) does not: %v\n"+
			"each one would mark something live that git never runs", len(invented), invented)
	}
}

// manPage returns the page with its overstrike removed.
//
// The formatted page spells every heading character twice around a backspace,
// so a plain match for "pre-push" finds the prose and not the heading. Undoing
// that in Go keeps the rule this fleet works under: a filter is a program, not
// a pipeline.
func manPage(section, name string) (string, error) {
	cmd := exec.Command("man", section, name)
	cmd.Env = append(os.Environ(), "MANWIDTH=200", "MANPAGER=cat", "PAGER=cat")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "", errors.New("the page was empty")
	}
	return deOverstrike(string(out)), nil
}

// deOverstrike drops each backspace together with the character it erases,
// which is how nroff writes bold and underline.
func deOverstrike(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\b' {
			if n := len(out); n > 0 {
				out = out[:n-1]
			}
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// hookHeadings returns the hook names the page gives a section of their own.
//
// ⛔ "An indented line holding nothing but a hook-shaped word" is not enough,
// and the way that showed is the point. It collected `rebase`, which is not a
// hook — it is a sub-case listed inside the post-rewrite section, since
// post-rewrite is run by `git commit --amend` and by `git rebase`. The test
// then reported a hook missing from the list, and the tempting repair was to
// add `rebase` or to exclude it by name: in both the oracle ends up fitted to
// the answer it is supposed to judge.
//
// The page settles it instead. Measured on git 2.54.0: every one of the 28
// hook headings sits at indentation 3, and `rebase` at 5. So the heading level
// is READ OFF the page — the indentation shared by the overwhelming majority
// of candidate lines — and nothing is named here that git does not name.
func hookHeadings(page string) []string {
	type candidate struct {
		name   string
		indent int
	}
	var cands []candidate
	byIndent := map[int]int{}
	for _, line := range strings.Split(page, "\n") {
		name := strings.TrimSpace(line)
		if !strings.HasPrefix(line, " ") || name == "" || !hookShaped(name) {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		cands = append(cands, candidate{name, indent})
		byIndent[indent]++
	}
	level, most := -1, 0
	for indent, n := range byIndent {
		if n > most || (n == most && indent < level) {
			level, most = indent, n
		}
	}

	var out []string
	seen := map[string]bool{}
	for _, c := range cands {
		if c.indent != level || seen[c.name] {
			continue
		}
		seen[c.name] = true
		out = append(out, c.name)
	}
	sort.Strings(out)
	return out
}

// hookShaped reports whether a word could be a hook name at all: lower-case
// letters, digits and hyphens, nothing else.
//
// ⚠ It deliberately does NOT require a hyphen. The first draft did, which
// would have rejected `update` — a real hook, and the one whose name looks
// least like one — and then reported it as a name this list invented. A filter
// written around the names one happens to remember re-introduces exactly the
// mistake this test is here to catch.
func hookShaped(s string) bool {
	if len(s) < 6 || len(s) > 30 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ⛔ The defect this file exists for, stated as the smallest case: the two
// binaries git runs on this machine sit in a directory PATH does not hold, and
// the report called that directory one nothing reads.
func TestGitRunsAHookByNameWhereThePathReachesNothing(t *testing.T) {
	hooks := "/home/someone/.config/git/hooks"
	if runsAsGitHook("pre-push", hooks, hooks) != true {
		t.Error("git runs <hooksPath>/pre-push on every push; this said nothing runs it")
	}
	if runsAsGitHook("post-checkout", hooks, hooks) != true {
		t.Error("git runs <hooksPath>/post-checkout on every checkout; this said nothing runs it")
	}
	if runsAsGitHook("pre-push", "/home/someone/.local/bin", hooks) {
		t.Error("a hook NAME outside the hooks path is an ordinary binary: git does not run it")
	}
}

// ⛔ The seven lines that made the directory unreadable. Each of these is a
// real file measured in ~/.config/git/hooks on 2026-11-22, each is truly an
// older release, and none of them is news: git cannot run any of them, because
// the name does not match.
func TestAnOlderHomonymOfTheToolIsProvablyInertAndNotMerelySuspicious(t *testing.T) {
	hooks := "/home/someone/.config/git/hooks"
	for _, name := range []string{
		"git-pre-push-guard",                // the TOOL's name, not the hook's
		"git-post-checkout-guard",           // idem
		"pre-push.bak-20261122-161700",      // the witness that a recopy happened
		"post-checkout.bak-20261011-104159", // an older witness
	} {
		if runsAsGitHook(name, hooks, hooks) {
			t.Errorf("%s: git runs a hook by its exact name, so this one it never runs", name)
		}
		if !inertInHooksDir(name, hooks, hooks) {
			t.Errorf("%s: it sits where git looks under a name git ignores — that is provably inert, "+
				"which is a stronger thing to print than \"behind\"", name)
		}
	}
	if inertInHooksDir("pre-push", hooks, hooks) {
		t.Error("pre-push is the hook itself; calling it inert would hide the only line that matters")
	}
	if inertInHooksDir("gitpush", "/home/someone/.local/bin", hooks) {
		t.Error("a binary on PATH is not inert: anything on PATH can be run by name")
	}
}

// ⛔ Unread is not negative. A git that cannot be asked is not a machine with
// no hooks — that reading is the oldest defect in this fleet.
func TestAGitThatCannotBeAskedIsNotAMachineWithoutHooks(t *testing.T) {
	boom := errors.New("exec: \"git\": executable file not found in $PATH")
	path, err := gitHooksPath(func(string, ...string) ([]byte, error) { return nil, boom })
	if err == nil {
		t.Fatal("git could not be run, and this returned no error: the caller would read the empty " +
			"path as \"nothing is configured\" and rank every hook as inert")
	}
	if path != "" {
		t.Errorf("no answer must not come back as an answer, got %q", path)
	}
}

// git exits 1 for a key that is not set. That IS an answer: git then uses each
// repository's own .git/hooks, and nothing is wrong.
func TestAnUnsetHooksPathIsAnAnswerAndNotAFailure(t *testing.T) {
	path, err := gitHooksPath(func(string, ...string) ([]byte, error) {
		return nil, exitOne(t)
	})
	if err != nil {
		t.Errorf("core.hooksPath unset is git answering, not git failing: %v", err)
	}
	if path != "" {
		t.Errorf("unset means unset, got %q", path)
	}
}

func TestTheConfiguredHooksPathIsReadWithoutItsNewline(t *testing.T) {
	path, err := gitHooksPath(func(string, ...string) ([]byte, error) {
		return []byte("/home/someone/.config/git/hooks\n"), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/home/someone/.config/git/hooks" {
		t.Errorf("got %q, want the path with no trailing newline", path)
	}
}

// A repository's own .git/hooks is a hooks directory even with core.hooksPath
// unset — which is the case on every clone in the farm.
func TestARepositorysOwnHooksDirectoryIsOneEvenWithNothingConfigured(t *testing.T) {
	if !runsAsGitHook("pre-commit", "/w/repo/.git/hooks", "") {
		t.Error("with core.hooksPath unset git uses <repo>/.git/hooks; this said nothing runs there")
	}
	if runsAsGitHook("pre-commit", "/w/repo/hooks", "") {
		t.Error("a directory merely CALLED hooks is not git's: the parent must be .git")
	}
	if inHooksDir("/w/repo/.git/hooks/old", "") {
		t.Error("git does not descend into subdirectories of the hooks path")
	}
}

// The rule TestOnPathReadsThePathAndNotTheName already holds onPath to; the
// hooks path is compared the same way, or the two halves of one verdict would
// disagree about what a directory is.
func TestTheHooksPathIsComparedAsADirectoryNotAsASpelling(t *testing.T) {
	dir := t.TempDir()
	spelled := filepath.Join(dir, ".", "")
	if !sameDir(spelled, dir) {
		t.Errorf("%q and %q are the same directory", spelled, dir)
	}
}

// ⛔ The empty hooks path, with the one witness that distinguishes the guard
// from its absence.
//
// A mutation sweep reported `a == "" || b == ""` as SURVIVED, and the first
// reading — "redundant, filepath.Abs of a temp dir never equals the current
// directory" — was true about the witness and wrong about the code.
// filepath.Abs("") returns the WORKING DIRECTORY. So with core.hooksPath
// unset, a run started from inside the hooks directory would find every file
// there in "git's hooks path", on the strength of where the shell happened to
// be. The witness has to be a directory that IS the working directory, which
// is the one case a t.TempDir() in another place can never produce.
func TestAnUnsetHooksPathMatchesNothingEvenFromInsideTheDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if sameDir(dir, "") || sameDir("", dir) {
		t.Error("core.hooksPath is unset: nothing is git's configured hooks path, least of all " +
			"wherever the command was started from")
	}
	if runsAsGitHook("pre-push", dir, "") {
		t.Error("an unset hooks path must not promote the working directory to git's — the " +
			"answer would change with the shell's cwd and nothing else")
	}
	if inertInHooksDir("anything", dir, "") {
		t.Error("and the same for the group on the other side of the verdict")
	}
}

// ⛔ The joining line, which nothing held. Two mutations of it — `live:
// onPath(t.dir)` alone, and `dead := false` — each SURVIVED the suite that
// covered every helper in this file. So the report itself is driven here, with
// the proxy and the hooks path handed in, and the assertions are about what
// comes out rather than about the fixture.
const inertHeading = "under a name git NEVER RUNS"

func hooksFixture(t *testing.T) (string, []tool, func(string) release) {
	t.Helper()
	hooks := t.TempDir()
	// Nothing is on PATH: the only reason anything here can be live is git.
	t.Setenv("PATH", "/nowhere/at/all")
	tools := []tool{
		{name: "pre-push", dir: hooks, path: "g/cmd/git-pre-push-guard",
			mod: "g", modVersion: "v0.59.0"},
		{name: "git-pre-push-guard", dir: hooks, path: "g/cmd/git-pre-push-guard",
			mod: "g", modVersion: "v0.40.0"},
		{name: "pre-push.bak-20261011-104159", dir: hooks, path: "g/cmd/git-pre-push-guard",
			mod: "g", modVersion: "v0.3.2-0.20261004153348-12d69ea6ed75"},
	}
	ask := func(string) release {
		return release{Mod: "g", Latest: "v0.60.0",
			Versions: []string{"v0.40.0", "v0.45.0", "v0.59.0", "v0.60.0"}}
	}
	return hooks, tools, ask
}

func TestTheReportMarksTheHookGitRunsAndSetsTheCopiesApart(t *testing.T) {
	hooks, tools, ask := hooksFixture(t)
	var buf strings.Builder
	problem := reportReleasesWith(&buf, tools, ask, hooks, nil)
	out := buf.String()

	if !problem {
		t.Error("pre-push is one release behind and git runs it on every push: that is the " +
			"finding, and the exit status must carry it")
	}
	cut := strings.Index(out, inertHeading)
	if cut < 0 {
		t.Fatalf("the copies git cannot run were not set apart at all:\n%s", out)
	}
	findings, copies := out[:cut], out[cut:]

	if !strings.Contains(findings, "→ pre-push") {
		t.Errorf("git executes <hooksPath>/pre-push, and PATH holds nothing here, so this is the "+
			"one line that must carry the arrow:\n%s", findings)
	}
	if strings.Contains(findings, "v0.40.0") {
		t.Errorf("git-pre-push-guard is the TOOL's name, not the hook's — git never runs it, so it "+
			"must not sit among the findings:\n%s", findings)
	}
	if !strings.Contains(copies, "v0.40.0") {
		t.Errorf("it is still reported, just apart — dropping it would hide a real file:\n%s", copies)
	}
	if strings.Contains(findings, "NOT a release") {
		t.Errorf("the .bak copy carries a pseudo-version, but git cannot run it either: a heading "+
			"that reads as a finding is the wrong place for it:\n%s", findings)
	}
	if !strings.Contains(copies, "not a released version") {
		t.Errorf("a pseudo-version is not 0 releases back, and must not be printed as a "+
			"distance:\n%s", copies)
	}
	if !strings.Contains(out, "(1 of them RUN)") {
		t.Errorf("the totals must count what RUNS, which is one of the three:\n%s", out)
	}
}

// ⭐ The comparator, at last driven rather than described.
//
// ⛔ TestWhatRunsComesFirstWhateverTheDistance is named for this rule and never
// calls reportReleases: it builds a fixture and asserts the fixture is
// well-built. A sweep says so plainly — `old[i].live != old[j].live` and
// `old[i].behind != old[j].behind` both SURVIVED. The rule matters doubly now,
// since what counts as live is what this change redefines: an arrow that sorts
// below forty-five releases of noise is an arrow nobody reads.
func TestWhatRunsSortsFirstAndThenTheGreatestDistance(t *testing.T) {
	hooks := t.TempDir()
	elsewhere := t.TempDir()
	t.Setenv("PATH", "/nowhere/at/all")
	tools := []tool{
		{name: "far-and-dead", dir: elsewhere, path: "f/cmd/far", mod: "f", modVersion: "v0.1.0"},
		{name: "near-and-dead", dir: elsewhere, path: "n/cmd/near", mod: "n", modVersion: "v0.4.0"},
		{name: "pre-push", dir: hooks, path: "g/cmd/git-pre-push-guard", mod: "g", modVersion: "v0.59.0"},
	}
	rels := map[string]release{
		"f": {Mod: "f", Latest: "v0.4.0",
			Versions: []string{"v0.1.0", "v0.2.0", "v0.3.0", "v0.4.0"}},
		"n": {Mod: "n", Latest: "v0.5.0", Versions: []string{"v0.4.0", "v0.5.0"}},
		"g": {Mod: "g", Latest: "v0.60.0", Versions: []string{"v0.59.0", "v0.60.0"}},
	}
	var buf strings.Builder
	reportReleasesWith(&buf, tools, func(m string) release { return rels[m] }, hooks, nil)
	out := buf.String()

	at := func(name string) int {
		i := strings.Index(out, name)
		if i < 0 {
			t.Fatalf("%s is missing from the report entirely:\n%s", name, out)
		}
		return i
	}
	// pre-push is ONE release behind and nothing is on PATH: it is first only
	// because git runs it.
	if at("pre-push") > at("far-and-dead") || at("pre-push") > at("near-and-dead") {
		t.Errorf("the only line something RUNS must lead, whatever the distance:\n%s", out)
	}
	// Among the ones nothing runs, the greater distance leads.
	if at("far-and-dead") > at("near-and-dead") {
		t.Errorf("three releases back must print above one:\n%s", out)
	}
	if !strings.Contains(out, "(1 of them RUN)") {
		t.Errorf("one of the three is run, by git:\n%s", out)
	}
}

// ⛔ And the exit status. A machine whose only stale binaries are copies git
// cannot run has nothing to do, and a gate that stays red regardless stops
// being read.
func TestCopiesGitCannotRunDoNotSetTheExitStatus(t *testing.T) {
	hooks, tools, ask := hooksFixture(t)
	var buf strings.Builder
	problem := reportReleasesWith(&buf, tools[1:], ask, hooks, nil)
	if problem {
		t.Errorf("the only files left are an older homonym and a .bak witness: nothing runs "+
			"either, so there is nothing to act on\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), inertHeading) {
		t.Error("exit 0 must not mean silence: they are still printed")
	}
}

// ⛔ Unread is not negative, at the one place this change could reintroduce it.
func TestAnUnaskableGitIsSaidBesideTheLinesItWouldHaveRanked(t *testing.T) {
	_, tools, ask := hooksFixture(t)
	var buf strings.Builder
	reportReleasesWith(&buf, tools, ask, "", errors.New("no git on this machine"))
	out := buf.String()
	if !strings.Contains(out, "UNPROVEN") {
		t.Errorf("git could not be asked where it looks and a scanned file is named pre-push: "+
			"the ranking above is unproven, and silence would present it as measured:\n%s", out)
	}
	if !strings.Contains(out, "no git on this machine") {
		t.Errorf("the reason belongs in the line, or the reader cannot act on it:\n%s", out)
	}
}

// exitOne produces a real *exec.ExitError with status 1 — what git returns for
// a key that is not set — so the seam is driven with the thing itself and not
// with a stand-in that happens to satisfy the type.
func exitOne(t *testing.T) error {
	t.Helper()
	err := exec.Command("false").Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("could not produce an exit status 1 to test with: %v", err)
	}
	return err
}
