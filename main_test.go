package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE DISTINCTION THIS TOOL EXISTS FOR. A CLI built with an older toolchain
// runs fine; a tool that PARSES GO SOURCE built with an older toolchain
// emits type errors about the standard library and then reports on whatever
// it managed to load. Only the second is worth waking somebody for.
func TestReadsGoSource(t *testing.T) {
	for name, c := range map[string]struct {
		deps []string
		want bool
	}{
		"a plain CLI":    {[]string{"github.com/spf13/cobra", "gopkg.in/yaml.v3"}, false},
		"x/tools itself": {[]string{"golang.org/x/tools"}, true},
		"a submodule":    {[]string{"golang.org/x/tools/gopls"}, true},
		"staticcheck":    {[]string{"honnef.co/go/tools"}, true},
		"nothing at all": {nil, false},
		// ⛔ THE SLASH IS THE RULE. A prefix match without it calls
		// "golang.org/x/toolsomething" a source reader — a different module
		// that merely starts the same way.
		"a lookalike module": {[]string{"golang.org/x/toolsomething"}, false},
	} {
		if got := readsGoSource(c.deps); got != c.want {
			t.Errorf("%s: readsGoSource(%v) = %v, want %v", name, c.deps, got, c.want)
		}
	}
}

// scan reads real build info, so the fixture is a real Go binary: this test
// binary, which has build info of its own and was built by the toolchain
// running the test.
func TestScanReadsARealBinary(t *testing.T) {
	dir := t.TempDir()
	self, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Skipf("cannot read the test binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "atool"), self, 0o755); err != nil {
		t.Fatal(err)
	}
	// NOT A GO BINARY, and skipped in SILENCE: these directories hold shell
	// scripts and symlinks, and a tool that complained about each one would
	// bury its own answer.
	if err := os.WriteFile(filepath.Join(dir, "a-shell-script"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tools, err := scan([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].name != "atool" {
		t.Fatalf("scanned %+v, want just atool", tools)
	}
	if !strings.HasPrefix(tools[0].goVersion, "go1.") {
		t.Errorf("goVersion = %q, want the toolchain that built this test", tools[0].goVersion)
	}
}

// A DIRECTORY THAT IS NOT THERE IS NOT AN ERROR: not every machine has both
// $GOBIN and ~/.local/bin, and failing on the absent one would make the
// default invocation useless on half of them.
func TestScanIgnoresAnAbsentDirectory(t *testing.T) {
	tools, err := scan([]string{filepath.Join(t.TempDir(), "nope")})
	if err != nil {
		t.Fatalf("an absent directory was an error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("found %d tools in a directory that does not exist", len(tools))
	}
}

// THE SOURCE READERS COME FIRST AND ARE NAMED AS THE ONES THAT MATTER. A
// page of old CLIs ahead of them would bury the eight that misreport.
func TestReportLeadsWithTheOnesThatMisreport(t *testing.T) {
	tools := []tool{
		{name: "oldcli", dir: "/a", goVersion: "go1.26.4", path: "example.com/oldcli"},
		{name: "oldlint", dir: "/a", goVersion: "go1.26.4", path: "example.com/oldlint", readsSource: true},
		{name: "current", dir: "/a", goVersion: "go1.27.1", path: "example.com/current"},
	}
	var b strings.Builder
	if !report(&b, tools, "go1.27.1", false, false) {
		t.Error("a stale source reader did not set the problem flag")
	}
	got := b.String()
	if !strings.Contains(got, "MISREPORT") || !strings.Contains(got, "oldlint") {
		t.Errorf("the source reader is not called out:\n%s", got)
	}
	// The plain old CLI is NOT in the headline section — it is noise there.
	headline, _, _ := strings.Cut(got, "\n\n")
	if strings.Contains(headline, "oldcli") {
		t.Errorf("an ordinary old CLI was raised as if it misreported:\n%s", headline)
	}
	// IT TELLS YOU WHAT TO RUN. A report that names a problem and not its
	// fix makes the reader go and look up the module path.
	if !strings.Contains(got, "go install example.com/oldlint@latest") {
		t.Errorf("no remedy given:\n%s", got)
	}
	// THE TOTALS ARE ALWAYS PRINTED, including the parts that are zero: a
	// command that says nothing when all is well cannot be told apart from
	// one that failed to look.
	if !strings.Contains(got, "3 Go tool(s), 2 behind go1.27.1, 1 of those read Go source") {
		t.Errorf("the totals are wrong or missing:\n%s", got)
	}
}

// A DUPLICATE IS ONLY A PROBLEM WHEN THE TOOLCHAINS DIFFER. The same tool
// installed twice from the same build is housekeeping, not a hazard, and
// reporting it would train people to ignore the section.
func TestDuplicatesAreReportedOnlyWhenTheyDisagree(t *testing.T) {
	same := []tool{
		{name: "t", dir: "/a", goVersion: "go1.27.1"},
		{name: "t", dir: "/b", goVersion: "go1.27.1"},
	}
	var b strings.Builder
	if report(&b, same, "go1.27.1", false, false) {
		t.Error("two identical copies were reported as a problem")
	}
	if strings.Contains(b.String(), "INSTALLED TWICE") {
		t.Errorf("identical copies were listed:\n%s", b.String())
	}

	differ := []tool{
		{name: "t", dir: "/a", goVersion: "go1.27.1"},
		{name: "t", dir: "/b", goVersion: "go1.26.4"},
	}
	b.Reset()
	if !report(&b, differ, "go1.27.1", false, false) {
		t.Error("a shadowed tool did not set the problem flag")
	}
	got := b.String()
	if !strings.Contains(got, "INSTALLED TWICE") || !strings.Contains(got, "PATH order") {
		t.Errorf("the shadowing is not explained:\n%s", got)
	}
	// BOTH directories are named: knowing which one is stale is the whole
	// of the fix.
	if !strings.Contains(got, "/a") || !strings.Contains(got, "/b") {
		t.Errorf("the two copies are not located:\n%s", got)
	}
}

// AN EMPTY RESULT SAYS SO. Walking a directory that holds no Go binaries
// and printing a bare "0" reads as "all clear"; it is more often "wrong
// directory".
func TestReportOfNothingSaysSo(t *testing.T) {
	var b strings.Builder
	if report(&b, nil, "go1.27.1", false, false) {
		t.Error("an empty scan was a problem")
	}
	if !strings.Contains(b.String(), "no Go binaries found") {
		t.Errorf("an empty scan did not question itself:\n%s", b.String())
	}
}

var _ io.Writer = (*strings.Builder)(nil)

// ⛔ Le défaut mesuré le 2026-10-17 sur une machine en go1.27.1: treize outils
// bâtis en go1.27.2 figuraient sous « READ GO SOURCE and are behind go1.27.1 —
// these do not fail, they MISREPORT ». Ils étaient EN AVANCE. Le test était
// `t.goVersion != want`, une inégalité de chaînes, et le mot de l'en-tête —
// « behind » — n'est pas une question à laquelle `!=` peut répondre.
func TestBehindMeansOlderAndNotMerelyDifferent(t *testing.T) {
	for _, c := range []struct {
		got, want string
		behind    bool
		why       string
	}{
		{"go1.27.1", "go1.27.1", false, "la même chaîne n'est pas en retard"},
		{"go1.26.4", "go1.27.1", true, "une mineure plus ancienne est en retard"},
		{"go1.27.2", "go1.27.1", false, "⛔ LE CAS MESURÉ: un correctif PLUS RÉCENT n'est pas en retard"},
		{"go1.28.0", "go1.27.1", false, "une mineure plus récente non plus"},
		{"go1.27.0", "go1.27.1", true, "un correctif plus ancien l'est"},
		// ⛔ Le piège qu'une comparaison lexicographique rate dans l'AUTRE sens:
		// "go1.27.10" < "go1.27.2" en octets, et c'est faux.
		{"go1.27.10", "go1.27.2", false, "10 est plus récent que 2, malgré l'ordre des octets"},
		{"go1.27.2", "go1.27.10", true, "et réciproquement"},
		// Une version illisible doit être RAPPORTÉE, pas passée en silence: on ne
		// sait pas ce que son analyseur comprend.
		{"", "go1.27.1", true, "une chaîne vide est rapportée plutôt que tue"},
		{"devel +abcdef", "go1.27.1", true, "une version de développement aussi"},
	} {
		if got := behind(c.got, c.want); got != c.behind {
			t.Errorf("behind(%q, %q) = %v, attendu %v — %s", c.got, c.want, got, c.behind, c.why)
		}
	}
}
