package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture is a small manifest in the contract's shape, in English titles to
// show that sections are found by position, not by text.
var Fixture = "# config\n\n" +
	"## Meta\n\n| Key | Value |\n| --- | --- |\n| kit_revisado | `2026-11-01` |\n| persistencia_cmd | " + emDash + " |\n\n" +
	"## Paths\n\n| Path | Value | What |\n| --- | --- | --- |\n| kit | .stele | the kit |\n| base | docs | the docs |\n\n" +
	"```text\n## not a section\n| x | y |\n```\n\n" +
	"## Names\n\n| Role | File |\n| --- | --- |\n| entry | guide.md |\n| state | now.md |\n| history_dir | hist/ |\n| manual | --- |\n\n" +
	"## Features\n\n| F | V |\n| --- | --- |\n| audit_every_n_sessions | 10 |\n\n" +
	"## Budgets\n\n| Doc | Max |\n| --- | --- |\n| entry | 5 |\n| state | 3 |\n\n" +
	"## Wording\n\n| R | P | T |\n| --- | --- | --- |\n"

// Write writes content as the manifest of a fresh folder and returns its path.
func Write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), DefaultName)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadSectionsByPosition(t *testing.T) {
	m, err := Read(Write(t, Fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Sections) != 6 || m.Section(Budgets).Title != "Budgets" || m.Section(Paths).Title != "Paths" {
		t.Fatalf("sections = %+v", m.Sections)
	}
	if v, ok := m.Value(Meta, "kit_revisado"); !ok || v != "2026-11-01" {
		t.Fatalf("kit_revisado = %q, %v", v, ok)
	}
	if v, _ := m.Value(Meta, "persistencia_cmd"); !IsSentinel(v) {
		t.Fatalf("persistencia_cmd = %q", v)
	}
	if _, ok := m.Value(Features, "update_every_n_days"); ok {
		t.Fatal("missing row reported as present")
	}
	if n := len(m.Section(Budgets).Rows); n != 2 {
		t.Fatalf("budget rows = %d", n)
	}
}

func TestRolePath(t *testing.T) {
	m, err := Read(Write(t, Fixture))
	if err != nil {
		t.Fatal(err)
	}
	for role, want := range map[string]string{
		"entry":    "docs/guide.md",
		"state":    "docs/hist/now.md",
		"handover": "docs/hist/handover.md", // no row: kit default
		"gotchas":  "docs/memory.md",
	} {
		if got, err := m.RolePath(role); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", role, got, err, want)
		}
	}
	if _, err := m.RolePath("manual"); !errors.Is(err, ErrDisabled) {
		t.Errorf("manual err = %v", err)
	}
	if _, err := m.RolePath("session"); err == nil {
		t.Error("session is a name pattern, not a document")
	}
}

func TestReadFailsOnTooFewSections(t *testing.T) {
	_, err := Read(Write(t, "## Meta\n\n## Paths\n"))
	if err == nil || !strings.Contains(err.Error(), "has 2 ## sections") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Read(filepath.Join(t.TempDir(), "none.md")); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestContainerPathAndName(t *testing.T) {
	m, err := Read(Write(t, Fixture))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := m.ContainerPath("history_dir"); err != nil || got != "docs/hist" {
		t.Errorf("history_dir = %q, %v", got, err)
	}
	if got, err := m.ContainerPath("correspondence_dir"); err != nil || got != "docs/correspondencia" {
		t.Errorf("correspondence_dir = %q, %v", got, err)
	}
	if _, err := m.ContainerPath("entry"); err == nil {
		t.Error("entry is not a container")
	}
	if got, err := m.Name("session"); err != nil || got != "sesion-{NNN}-{YYYY-MM-DD}.md" {
		t.Errorf("session = %q, %v", got, err)
	}
	if _, err := m.Name("manual"); !errors.Is(err, ErrDisabled) {
		t.Errorf("manual err = %v", err)
	}
}
