package main

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// ⛔ "What runs it" is not "what the shell searches", and this tool's own
// founding case is the proof.
//
// #4 added -releases because the global pre-push hook was built from v0.3.2
// while its repository was at v0.6.3. It then ranked every line by PATH
// membership — and PATH does not contain git's hooks directory. Measured on
// 2026-11-22, `staletools -releases ~/.config/git/hooks` printed:
//
//	an OLDER RELEASE of themselves — what runs is not what the repository says.
//	→ marks the 0 in a directory the shell SEARCHES; the other 9 are in one nothing reads:
//
// Nine lines, no arrow, and two of the nine were `pre-push` and
// `post-checkout` — the binaries git executes on every push and every
// checkout on this machine, ranked among seven backup copies. The heading was
// not merely unhelpful, it was FALSE: git reads that directory. The tool
// written because of a stale git hook demoted the stale git hook.
//
// So liveness has two sources, and they are read differently. The shell runs
// a file because its DIRECTORY is on PATH, whatever the file is called; git
// runs a file because its NAME is a hook name AND its directory is the hooks
// path. The second needs the name, which is what gitHookNames is for.

// gitHookNames are the names git executes, from githooks(5).
//
// ⛔ A maintained list of names is what this tool's package comment refuses
// for source readers — "evidence out of the binary, not a list of names
// somebody maintained". Two things that are not true there make it
// admissible here. The list is GIT'S: it is fixed by the installed git, not
// by a fashion in module names. And it fails in the safe direction — a name
// missing from it demotes a live hook into the quiet group, which is the
// error this change exists to fix and never a new false arrow.
//
// It also buys a statement no heuristic gives. A file sitting in the hooks
// path under a name that is NOT here is one git CANNOT run: provably inert,
// rather than "looks like a backup".
//
// ⚠ The list below is not written from memory. The first draft of it was, and
// it carried `proc-receive-pre-remote`, a name git has never had.
// TestTheHookNameListIsGitsOwnAndNotMine reads githooks(5) from the git
// installed on the machine and compares both directions, so the page is the
// authority and this comment is not.
var gitHookNames = map[string]bool{
	"applypatch-msg":        true,
	"pre-applypatch":        true,
	"post-applypatch":       true,
	"pre-commit":            true,
	"pre-merge-commit":      true,
	"prepare-commit-msg":    true,
	"commit-msg":            true,
	"post-commit":           true,
	"pre-rebase":            true,
	"post-checkout":         true,
	"post-merge":            true,
	"pre-push":              true,
	"pre-receive":           true,
	"update":                true,
	"proc-receive":          true,
	"post-receive":          true,
	"post-update":           true,
	"reference-transaction": true,
	"push-to-checkout":      true,
	"pre-auto-gc":           true,
	"post-rewrite":          true,
	"sendemail-validate":    true,
	"fsmonitor-watchman":    true,
	"p4-changelist":         true,
	"p4-prepare-changelist": true,
	"p4-post-changelist":    true,
	"p4-pre-submit":         true,
	"post-index-change":     true,
}

// runner is the seam the tests drive. It is the one call this file makes.
type runner func(name string, arg ...string) ([]byte, error)

func execRunner(dir string) runner {
	return func(name string, arg ...string) ([]byte, error) {
		cmd := exec.Command(name, arg...)
		cmd.Dir = dir
		return cmd.Output()
	}
}

// gitHooksPath asks git where it looks for hooks, outside any repository, so
// the answer is the global one.
//
// ⛔ Three answers, not two, and the third is the one this fleet keeps
// getting wrong. "" with a nil error means git was asked and nothing is
// configured — git then uses each repository's own .git/hooks, which
// isRepoHooksDir covers. "" with an error means THE QUESTION COULD NOT BE
// PUT, and a caller that reads that as "no hooks directory" repeats the
// oldest defect here: a scan that could not read, reported as a clean
// machine. The caller says so instead.
func gitHooksPath(run runner) (string, error) {
	out, err := run("git", "config", "--get", "core.hooksPath")
	if err != nil {
		// git exits 1 for "this key is not set", which is an answer and
		// not a failure. Anything else — no git, a broken config — is the
		// question going unanswered.
		var ee *exec.ExitError
		if asExitError(err, &ee) && ee.ExitCode() == 1 && len(out) == 0 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// asExitError is errors.As, spelled out so this file keeps its single import
// of os/exec and needs no other dependency — the module still has none.
func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// sameDir compares directories by what they are, not how they are spelled —
// the rule TestOnPathReadsThePathAndNotTheName already holds onPath to.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	// ⛔ The errors are dropped on purpose, and the branch that read them is
	// gone. filepath.Abs fails only when the working directory cannot be
	// read, and then it fails for BOTH arguments alike — so the fallback
	// `return a == b` was a second code path that no input could tell from
	// its own absence: a mutation sweep reported it SURVIVED, and there is no
	// witness to write. The guard above is what makes this safe, since Abs of
	// a non-empty path is non-empty.
	p, _ := filepath.Abs(a)
	q, _ := filepath.Abs(b)
	return p == q
}

// isRepoHooksDir reports whether a directory is a repository's own .git/hooks,
// which git uses whenever core.hooksPath is unset.
func isRepoHooksDir(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return filepath.Base(abs) == "hooks" && filepath.Base(filepath.Dir(abs)) == ".git"
}

// inHooksDir reports whether git looks in this directory at all.
func inHooksDir(dir, hooksPath string) bool {
	return sameDir(dir, hooksPath) || isRepoHooksDir(dir)
}

// runsAsGitHook reports whether git executes this file: the right directory
// AND a name git knows.
func runsAsGitHook(name, dir, hooksPath string) bool {
	return gitHookNames[name] && inHooksDir(dir, hooksPath)
}

// inertInHooksDir reports whether the file sits where git looks under a name
// git never runs — a copy, a homonym of the TOOL rather than of the hook, or
// a .bak witness that a recopy happened.
//
// ⛔ This is the group that made the hooks directory unreadable: on this
// machine it is seven of nine lines, and each is true and none is news.
// Measured there: `git-pre-push-guard` v0.40.0 and `git-post-checkout-guard`
// v0.36.0 — older homonyms of the tool, which git never runs because the
// names do not match — beside five `.bak-<stamp>` copies.
func inertInHooksDir(name, dir, hooksPath string) bool {
	return !gitHookNames[name] && inHooksDir(dir, hooksPath)
}
