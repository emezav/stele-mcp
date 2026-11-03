package cadence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func config(meta, features string) string {
	return "## Meta\n\n| P | V |\n| --- | --- |\n" + meta + "\n## Rutas\n\n| R | V |\n| --- | --- |\n| base | docs |\n\n" +
		"## Nombres\n\n## Features\n\n| F | V |\n| --- | --- |\n" + features + "\n## Presupuestos\n\n## Wording\n"
}

const (
	meta     = "| kit_origen | `https://example.com/kit` |\n| kit_revisado | `2026-11-01` |\n"
	features = "| audit_log | on |\n| audit_every_n_sessions | 10 |\n| update_every_n_days | 30 |\n"
	index    = "| N | Fecha |\n| --- | --- |\n| 019 | 2026-11-01 |\n| 021 | 2026-11-02 |\n| 020 | 2026-11-01 |\n"
	audits   = "# audit\n\n| Audit | Fecha | Sesiones | Alcance |\n| --- | --- | --- | --- |\n| 1 | 2026-10-01 | 001-010 | completa |\n"
)

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

func TestCheckBothDueAndNot(t *testing.T) {
	root := instance(t, config(meta, features), map[string]string{
		"docs/history/index.md": index, "docs/history/audit.md": audits,
	})
	res, err := Check(root, Options{Today: "2026-12-02"})
	if err != nil {
		t.Fatal(err)
	}
	a, k := res.Audit, res.Kit
	if res.OK || a.Status != Overdue || a.Session != 21 || a.LastCovered != 10 || a.Since != 11 || a.Audits != 1 ||
		a.Log != "docs/history/audit.md" || k.Status != Overdue || k.Days != 31 {
		t.Fatalf("res = %+v", res)
	}
	res, err = Check(root, Options{Today: "2026-12-01", Session: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Audit.Status != OK || res.Kit.Status != OK || res.Kit.Days != 30 ||
		!strings.Contains(res.Audit.Advice, "closing session 21") || !strings.Contains(res.Kit.Advice, "after 2026-12-01") {
		t.Fatalf("res = %+v", res)
	}
}

func TestCheckNeverAudited(t *testing.T) {
	root := instance(t, config(meta, features), map[string]string{"docs/history/index.md": index})
	res, err := Check(root, Options{Today: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Audit.Status != Overdue || res.Audit.Audits != 0 || res.Audit.Since != 21 || !strings.Contains(res.Audit.Advice, "never audited") {
		t.Fatalf("res = %+v", res.Audit)
	}
	res, _ = Check(root, Options{Today: "2026-11-02", Session: 4})
	if res.Audit.Status != OK {
		t.Fatalf("res = %+v", res.Audit)
	}
}

func TestCheckOffAndSentinels(t *testing.T) {
	root := instance(t, config("| kit_origen | --- |\n| kit_revisado | 2026-11-01 |\n", "| audit_log | off |\n"), map[string]string{})
	res, err := Check(root, Options{Today: "2027-01-01"})
	if err != nil || !res.OK || res.Audit.Status != Off || res.Kit.Status != NoReminder {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	root = instance(t, config(meta, "| audit_every_n_sessions | - |\n| update_every_n_days | --- |\n"), map[string]string{})
	res, err = Check(root, Options{Today: "2027-01-01"})
	if err != nil || res.Audit.Status != NoReminder || res.Kit.Status != NoReminder {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestCheckFails(t *testing.T) {
	ok := map[string]string{"docs/history/index.md": index}
	for name, c := range map[string]struct {
		cfg   string
		files map[string]string
		today string
	}{
		"bad today":    {config(meta, features), ok, "02/12/2026"},
		"bad cadence":  {config(meta, "| audit_every_n_sessions | diez |\n"), ok, ""},
		"bad reviewed": {config("| kit_origen | x |\n| kit_revisado | ayer |\n", "| audit_log | off |\n"), ok, ""},
		"no index":     {config(meta, features), map[string]string{}, ""},
		"bad range":    {config(meta, features), map[string]string{"docs/history/index.md": index, "docs/history/audit.md": strings.Replace(audits, "001-010", "todas", 1)}, ""},
	} {
		root := instance(t, c.cfg, c.files)
		if _, err := Check(root, Options{Today: c.today}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
