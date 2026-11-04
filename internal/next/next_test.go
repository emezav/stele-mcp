package next

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const config = "## Meta\n\n## Rutas\n\n| R | V |\n| --- | --- |\n| base | docs |\n\n" +
	"## Nombres\n\n| R | A |\n| --- | --- |\n| session | session-{NNNN}-{YYYY-MM-DD}.md |\n| history_dir | h/ |\n\n" +
	"## Features\n\n## Presupuestos\n\n## Wording\n"

const index = "| N | Date |\n| --- | --- |\n| 1 | 2026-11-01 |\n| 2 | 2026-11-02 |\n"

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

func TestCheckComposesNext(t *testing.T) {
	root := instance(t, config, map[string]string{
		"docs/h/index.md": index, "docs/h/esfuerzo.md": "| N |\n| --- |\n",
		"docs/h/session-0001-2026-11-01.md": "", "docs/h/session-0002-2026-11-02.md": "",
		"docs/h/session-notes.md": "", "docs/h/other-0009-2026-11-01.md": "",
	})
	res, err := Check(root, Options{Today: "2026-11-03"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Number != 3 || res.Padded != "0003" || res.Path != "docs/h/session-0003-2026-11-03.md" ||
		res.Index != "docs/h/index.md" || res.Effort != "docs/h/esfuerzo.md" || res.DateVerdict != "given" ||
		len(res.LastFiles) != 1 || res.LastFiles[0] != "session-0002-2026-11-02.md" {
		t.Fatalf("res = %+v", res)
	}
}

func TestCheckHalfDoneClose(t *testing.T) {
	// The row of session 3 was appended, its record was not written.
	root := instance(t, config, map[string]string{
		"docs/h/index.md":                   index + "| 3 | 2026-11-03 |\n",
		"docs/h/session-0001-2026-11-01.md": "", "docs/h/session-0002-2026-11-02.md": "",
	})
	res, err := Check(root, Options{Today: "2026-11-03"})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.IndexLast != 3 || res.FilesLast != 2 || res.Number != 4 || !strings.Contains(res.Advice, "without its record") || res.Effort != "" {
		t.Fatalf("res = %+v", res)
	}
	// And the other way round.
	root = instance(t, config, map[string]string{
		"docs/h/index.md": index, "docs/h/session-0003-2026-11-03.md": "",
	})
	res, err = Check(root, Options{Today: "2026-11-03"})
	if err != nil || res.OK || res.FilesLast != 3 || !strings.Contains(res.Advice, "without its row") {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestCheckFirstSessionAndMeasuredDate(t *testing.T) {
	root := instance(t, strings.Replace(config, "| session | session-{NNNN}-{YYYY-MM-DD}.md |\n", "", 1),
		map[string]string{"docs/h/index.md": "| N | Date |\n| --- | --- |\n"})
	res, err := Check(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Number != 1 || res.Padded != "001" || !strings.HasPrefix(res.File, "sesion-001-") || res.DateVerdict == "given" || res.Date == "" {
		t.Fatalf("res = %+v", res)
	}
}

func TestCheckFails(t *testing.T) {
	for name, c := range map[string]struct {
		cfg   string
		files map[string]string
		today string
	}{
		"no token":   {strings.Replace(config, "{NNNN}", "N", 1), map[string]string{"docs/h/index.md": index}, "2026-11-03"},
		"no index":   {config, map[string]string{"docs/h/x.md": ""}, "2026-11-03"},
		"bad number": {config, map[string]string{"docs/h/index.md": index + "| tres | x |\n"}, "2026-11-03"},
		"bad today":  {config, map[string]string{"docs/h/index.md": index}, "3/11/2026"},
	} {
		root := instance(t, c.cfg, c.files)
		if _, err := Check(root, Options{Today: c.today}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
