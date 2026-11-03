package handover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	args = append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo creates a repository with two empty commits and returns it and the
// hashes, oldest first.
func repo(t *testing.T) (string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	run(t, dir, "commit", "-q", "--allow-empty", "--no-verify", "-m", "one")
	first := run(t, dir, "rev-parse", "HEAD")
	run(t, dir, "commit", "-q", "--allow-empty", "--no-verify", "-m", "two")
	return dir, first, run(t, dir, "rev-parse", "HEAD")
}

func write(t *testing.T, dir, state, seal string) string {
	t.Helper()
	p := filepath.Join(dir, "handover.md")
	text := "# handover.md\n\n## Estado\n\n" + state + "\n\n## Sello\n\n" + seal + "\n\n## Objetivo\n\n`abcdef1234` not the seal\n"
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckVerdicts(t *testing.T) {
	dir, first, head := repo(t)
	for name, c := range map[string]struct {
		state, seal, verdict string
		active, ancestor     bool
		behind               int
	}{
		"current, short and quoted": {"SIN_TRABAJO_ACTIVO", "`HEAD` = `" + head[:7] + "` al cerrar (abrio en `" + first[:7] + "`)", Current, false, false, 0},
		"stale, bare full hash":     {"**EN_PROGRESO**", first + " (al abrir)", Stale, true, true, 1},
		"unknown":                   {"EN_PROGRESO", "`1234567`", Unknown, true, false, 0},
		"no seal":                   {"SIN_TRABAJO_ACTIVO", "sin VCS: los dos archivos en disco", NoSeal, false, false, 0},
	} {
		p := write(t, dir, c.state, c.seal)
		res, err := Check(dir, Options{Path: p})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Verdict != c.verdict || res.Active != c.active || res.Ancestor != c.ancestor || res.Behind != c.behind ||
			res.OK != (c.verdict == Current) || res.Head != head {
			t.Errorf("%s: res = %+v", name, res)
		}
	}
}

func TestCheckSealLineAndDirty(t *testing.T) {
	dir, _, head := repo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "f.txt")
	run(t, dir, "commit", "-q", "--no-verify", "-m", "three")
	head = run(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := write(t, dir, "EN_PROGRESO", head[:9])
	res, err := Check(dir, Options{Path: p})
	if err != nil {
		t.Fatal(err)
	}
	// handover.md itself is untracked and not counted; f.txt is.
	if res.Verdict != Current || res.Dirty != 1 || res.SealLine != 9 || res.Seal != head[:9] {
		t.Fatalf("res = %+v", res)
	}
}

func TestCheckNoVCS(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	res, err := Check(dir, Options{Path: write(t, dir, "EN_PROGRESO", "`abcdef1`")})
	if err != nil || res.Verdict != NoVCS || res.OK {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestCheckFromManifest(t *testing.T) {
	dir, _, head := repo(t)
	cfg := "## Meta\n\n## Rutas\n\n| R | V |\n| --- | --- |\n| base | docs |\n\n## Nombres\n\n| R | A |\n| --- | --- |\n| history_dir | h/ |\n\n## Features\n\n## Presupuestos\n\n## Wording\n"
	if err := os.WriteFile(filepath.Join(dir, "stele.config.md"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs", "h"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "docs", "h"), "SIN_TRABAJO_ACTIVO", "`"+head[:7]+"`")
	res, err := Check(dir, Options{})
	if err != nil || res.Verdict != Current || !strings.HasSuffix(res.Handover, "docs/h/handover.md") {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestCheckFails(t *testing.T) {
	dir := t.TempDir()
	if _, err := Check(dir, Options{}); err == nil || !strings.Contains(err.Error(), "manifest does not exist") {
		t.Fatalf("err = %v", err)
	}
	p := filepath.Join(dir, "h.md")
	if err := os.WriteFile(p, []byte("# h\n\n## Sello\n\n`abcdef1`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(dir, Options{Path: p}); err == nil || !strings.Contains(err.Error(), "no ## Estado") {
		t.Fatalf("err = %v", err)
	}
}
