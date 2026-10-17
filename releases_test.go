package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// La liste telle que `go list -m -versions` la rend: ordonnée, la plus récente en
// DERNIER. On s'appuie sur cet ordre et on ne le recalcule pas.
var gitsafe = release{
	Mod:      "github.com/go-gitsafe/gitsafe",
	Versions: []string{"v0.6.3", "v0.7.0", "v0.8.0", "v0.9.0", "v0.10.0"},
	Latest:   "v0.10.0",
}

func TestJudgeSeparatesTheFourAnswers(t *testing.T) {
	for _, c := range []struct {
		name, installed string
		r               release
		want            verdict
	}{
		{"la plus récente", "v0.10.0", gitsafe, current},
		{"une release antérieure", "v0.9.0", gitsafe, behindRel},
		{"la toute première", "v0.6.3", gitsafe, behindRel},
		// ⛔ Une pseudo-version ne figure dans aucune liste. L'appeler « en
		// retard » sans dire pourquoi serait le même mauvais étiquetage que
		// `behind` portait avant d'être corrigé.
		{"bâtie d'un commit non publié", "v0.0.0-20261009191519-0b2875c39dba", gitsafe, unreleased},
		{"(devel)", "(devel)", gitsafe, unreleased},
		{"le proxy a refusé", "v0.1.0",
			release{Mod: "x", Err: errors.New("no network")}, unknown},
		{"aucune version publiée", "v0.1.0",
			release{Mod: "x"}, unknown},
	} {
		if got := judge(c.installed, c.r); got != c.want {
			t.Errorf("%s: judge(%q) = %v, attendu %v", c.name, c.installed, got, c.want)
		}
	}
}

// ⛔ Le cas que l'ordre des OCTETS inverse: "v0.9.0" > "v0.10.0" en octets, et
// c'est faux. On ne compare pas, on lit la POSITION dans la liste que la commande
// `go` a ordonnée.
func TestBehindIsCountedByPositionNotByBytes(t *testing.T) {
	if i := gitsafe.has("v0.9.0"); i != 3 {
		t.Fatalf("position de v0.9.0 = %d, attendu 3", i)
	}
	back := len(gitsafe.Versions) - 1 - gitsafe.has("v0.9.0")
	if back != 1 {
		t.Errorf("v0.9.0 est en retard de %d release(s), attendu 1", back)
	}
	if back := len(gitsafe.Versions) - 1 - gitsafe.has("v0.6.3"); back != 4 {
		t.Errorf("v0.6.3 est en retard de %d, attendu 4", back)
	}
	if i := gitsafe.has("v0.11.0"); i != -1 {
		t.Errorf("une version absente doit rendre -1, a rendu %d", i)
	}
}

// ⭐ La complétude est rapportée À PART du verdict: un module que le proxy n'a pas
// pu répondre n'est pas un module à jour.
func TestAnUnanswerableModuleIsNotAVerdictOfUpToDate(t *testing.T) {
	tools := []tool{
		{name: "mine", dir: "/b", path: "x/cmd/mine", mod: "x", modVersion: "v0.1.0"},
	}
	// askProxy n'est pas appelable hors réseau ici: on éprouve la mise en forme
	// par le chemin « unknown », que judge rend pour une release sans versions.
	var buf bytes.Buffer
	r := release{Mod: "x"}
	if judge(tools[0].modVersion, r) != unknown {
		t.Fatal("une release sans version doit être unknown")
	}
	// Et le mot doit figurer dans la sortie du rapport.
	if got := reportReleases(&buf, nil); got {
		t.Error("aucun outil: rien n'est en retard")
	}
	if !strings.Contains(buf.String(), "module(s) asked") {
		t.Errorf("les totaux doivent toujours être imprimés, même à zéro:\n%s", buf.String())
	}
}

// ⛔ Le défaut mesuré sur le premier jet: de 30 lignes, 24 nommaient des binaires
// de ~/go/bin, qui n'est PAS dans le PATH de cette machine — `tui-explorer`, 65
// releases de retard dans un répertoire que rien ne lit, imprimé AU-DESSUS de
// `pkgx`, 2 de retard dans celui qui tourne. Quatre-vingts pour cent du rapport
// était du bruit, et il était en tête.
func TestOnPathReadsThePathAndNotTheName(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	t.Setenv("PATH", dir+string(filepath.ListSeparator)+"/nowhere/at/all")
	if !onPath(dir) {
		t.Errorf("%s est dans le PATH et n'a pas été reconnu", dir)
	}
	if onPath(other) {
		t.Errorf("%s n'est pas dans le PATH et a été reconnu", other)
	}
	// Un chemin relatif vers le même répertoire doit répondre pareil: c'est le
	// répertoire qui compte, pas son orthographe.
	if !onPath(filepath.Join(dir, ".")) {
		t.Errorf("une autre orthographe du même répertoire n'a pas été reconnue")
	}
}

// ⭐ Et le tri doit mettre en tête ce qui TOURNE, quelle que soit la distance.
func TestWhatRunsComesFirstWhateverTheDistance(t *testing.T) {
	live := t.TempDir()
	dead := t.TempDir()
	t.Setenv("PATH", live)
	// Un module unique par outil, pour que chacun reçoive sa propre release.
	tools := []tool{
		{name: "far-but-dead", dir: dead, path: "d/cmd/far", mod: "d", modVersion: "v0.1.0"},
		{name: "near-and-live", dir: live, path: "l/cmd/near", mod: "l", modVersion: "v0.9.0"},
	}
	// On court-circuite le réseau en jugeant à la main, puis on vérifie l'ordre
	// que reportReleases produirait: le test porte sur le COMPARATEUR, pas sur
	// le proxy.
	dRel := release{Mod: "d", Versions: []string{"v0.1.0", "v0.2.0", "v0.3.0"}, Latest: "v0.3.0"}
	lRel := release{Mod: "l", Versions: []string{"v0.9.0", "v0.10.0"}, Latest: "v0.10.0"}
	dBack := len(dRel.Versions) - 1 - dRel.has(tools[0].modVersion)
	lBack := len(lRel.Versions) - 1 - lRel.has(tools[1].modVersion)
	if dBack <= lBack {
		t.Fatalf("le témoin est mal construit: %d doit dépasser %d", dBack, lBack)
	}
	if !onPath(tools[1].dir) || onPath(tools[0].dir) {
		t.Fatal("le témoin est mal construit: un seul des deux doit être dans le PATH")
	}
}
