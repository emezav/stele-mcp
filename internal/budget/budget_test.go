package budget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const config = "## Meta\n\n## Rutas\n\n| Ruta | Valor |\n| --- | --- |\n| base | docs |\n\n" +
	"## Nombres\n\n| Rol | Archivo |\n| --- | --- |\n| history_dir | history/ |\n| manual | - |\n\n" +
	"## Features\n\n" +
	"## Presupuestos\n\n| Doc (rol) | Max. lineas |\n| --- | --- |\n" +
	"| entry | 5 |\n| gotchas | 3 |\n| state | 2 |\n| handover | --- |\n| manual | 9 |\n" +
	"| Plantilla del entry (del kit) | 150 |\n\n" +
	"## Wording\n"

// instance writes the manifest and the named files under a fresh root.
func instance(t *testing.T, cfg string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["stele.config.md"] = cfg
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCheckMeasuresAndDiscriminates(t *testing.T) {
	root := instance(t, config, map[string]string{
		"docs/guia-agente.md":      "1\n2\n3\n4\n5\n",         // exactly at budget
		"docs/memory.md":           "1\n2\n3\n4",              // 4 lines, last without newline
		"docs/history/latest.md":   "1\r\n2\r\n",              // CRLF counts the same
		"docs/history/handover.md": strings.Repeat("x\n", 99), // sentinel budget
	})
	res, err := Check(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Section != "Presupuestos" || len(res.Budgets) != 3 {
		t.Fatalf("res = %+v", res)
	}
	want := []Budget{
		{Role: "entry", File: "docs/guia-agente.md", Lines: 5, Max: 5, OK: true},
		{Role: "gotchas", File: "docs/memory.md", Lines: 4, Max: 3, Over: 1},
		{Role: "state", File: "docs/history/latest.md", Lines: 2, Max: 2, OK: true},
	}
	for i, w := range want {
		g := res.Budgets[i]
		g.Line = 0
		if g != w {
			t.Errorf("budget %d = %+v, want %+v", i, g, w)
		}
	}
	reasons := map[string]string{}
	for _, s := range res.Skipped {
		reasons[s.Key] = s.Reason
	}
	if len(reasons) != 3 || !strings.Contains(reasons["handover"], "sentinel") ||
		!strings.Contains(reasons["manual"], "disabled") || !strings.Contains(reasons["Plantilla del entry (del kit)"], "not a document role") {
		t.Fatalf("skipped = %+v", res.Skipped)
	}
}

func TestCheckAllWithin(t *testing.T) {
	root := instance(t, config, map[string]string{
		"docs/guia-agente.md": "1\n", "docs/memory.md": "", "docs/history/latest.md": "1\n",
	})
	res, err := Check(root, Options{})
	if err != nil || !res.OK {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestCheckReportsMissingDocument(t *testing.T) {
	root := instance(t, config, map[string]string{"docs/guia-agente.md": "1\n", "docs/memory.md": "1\n"})
	res, err := Check(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !res.Budgets[2].Missing || res.Budgets[2].OK {
		t.Fatalf("res = %+v", res)
	}
}

func TestCheckFails(t *testing.T) {
	for name, cfg := range map[string]string{
		"not a number":     strings.Replace(config, "| entry | 5 |", "| entry | 5 lineas |", 1),
		"nothing measured": "## Meta\n\n## Rutas\n\n## Nombres\n\n## Features\n\n## Presupuestos\n\n| Doc | Max |\n| --- | --- |\n| SKILL.md | 400 |\n\n## Wording\n",
	} {
		root := instance(t, cfg, map[string]string{})
		if _, err := Check(root, Options{}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := Check(t.TempDir(), Options{}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("no manifest: err = %v", err)
	}
}
