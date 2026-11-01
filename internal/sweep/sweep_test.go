package sweep

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emezav/markgrep/search"
)

func corpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"memory.md": "# memory\n\nThe built-in search does not\nsee the ignored folder.\n",
		"latest.md": "# latest\n\n- next step: implement sweep\n",
		"notes.txt": "not markdown: ignored folder\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPhraseSplitByLineBreak(t *testing.T) {
	dir := corpus(t)
	res, err := Run(dir, []string{"does not see"}, Options{Literal: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 2 || res.Matched != 1 || res.Truncated {
		t.Fatalf("result = %+v", res)
	}
	m := res.Matches[0]
	if m.Path != "memory.md" || m.Line != 3 || m.Kind != "paragraph" || m.Heading != "# memory" {
		t.Fatalf("match = %+v", m)
	}
	if m.Text != "The built-in search does not\nsee the ignored folder." {
		t.Fatalf("text = %q", m.Text)
	}
}

func TestExcludesAccumulate(t *testing.T) {
	dir := corpus(t)
	res, err := Run(dir, []string{"memory|latest"}, Options{Exclude: []string{"memory.md", "latest.md"}})
	if !errors.Is(err, search.ErrEmptyCorpus) {
		t.Fatalf("every file excluded: err = %v, result = %+v", err, res)
	}
	res, err = Run(dir, []string{"#"}, Options{Literal: true, Exclude: []string{"memory.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0] != "latest.md" {
		t.Fatalf("files = %v", res.Files)
	}
}

func TestRefusalsAreErrors(t *testing.T) {
	dir := corpus(t)
	if _, err := Run(dir, []string{"sweep"}, Options{Exclude: []string{"nothere*.md"}}); !errors.Is(err, search.ErrFilterMatchesNothing) {
		t.Fatalf("dead filter: err = %v", err)
	}
	if _, err := Run(dir, []string{"sweep"}, Options{Control: []string{"no such phrase"}}); !errors.Is(err, search.ErrControlFailed) {
		t.Fatalf("failed control: err = %v", err)
	}
	if _, err := Run(dir, nil, Options{}); err == nil {
		t.Fatal("no patterns: expected an error")
	}
	if _, err := Run("", []string{"sweep"}, Options{}); err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("no path: err = %v", err)
	}
	if _, err := Run(t.TempDir(), []string{"sweep"}, Options{}); !errors.Is(err, search.ErrEmptyCorpus) {
		t.Fatalf("no Markdown under root: err = %v", err)
	}
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "empty.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(empty, []string{"sweep"}, Options{}); !errors.Is(err, search.ErrEmptyCorpus) {
		t.Fatalf("one file, no unit: err = %v", err)
	}
	if _, err := Run(dir, []string{"sweep"}, Options{Kind: "table"}); !errors.Is(err, search.ErrFilterMatchesNothing) {
		t.Fatalf("kind with no unit of its type: err = %v", err)
	}
}

func TestNoMatchIsEmptyNotNil(t *testing.T) {
	dir := corpus(t)
	res, err := Run(dir, []string{"absent phrase"}, Options{Control: []string{"next step"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 0 || res.Matches == nil || len(res.Files) != 2 || res.Units == 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestLimitIsReported(t *testing.T) {
	dir := corpus(t)
	res, err := Run(dir, []string{"#"}, Options{Literal: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 2 || len(res.Matches) != 1 || !res.Truncated {
		t.Fatalf("result = %+v", res)
	}
}

func TestPathsRelativeToRoot(t *testing.T) {
	dir := corpus(t)
	sub := filepath.Join(dir, "temas")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "append-row.md"), []byte("sweep here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(dir, []string{"sweep"}, Options{Literal: true})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, m := range res.Matches {
		paths = append(paths, m.Path)
	}
	if strings.Join(paths, ",") != "latest.md,temas/append-row.md" {
		t.Fatalf("paths = %v, files = %v", paths, res.Files)
	}

	file := filepath.Join(dir, "latest.md")
	res, err = Run(file, []string{"sweep"}, Options{Literal: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches[0].Path != filepath.ToSlash(file) || res.Files[0] != filepath.ToSlash(file) {
		t.Fatalf("root is the file: result = %+v", res)
	}
}
