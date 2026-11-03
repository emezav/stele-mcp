package comments

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type hit struct {
	line, col int
	kind      string
}

func scan(t *testing.T, text string) ([]hit, int) {
	t.Helper()
	var hits []hit
	n := Scan(text, func(f Finding) { hits = append(hits, hit{f.Line, f.Column, f.Kind}) })
	return hits, n
}

func TestScanWellFormed(t *testing.T) {
	text := "# t\n\n<!-- one -->\n\ntext <!-- two\nlines --> more <!---->\n" +
		"`<!--` and `` --> `` in code spans\n\n```html\n<!-- in a fence\n```\n\n~~~~\n--> too\n~~~~\n"
	hits, n := scan(t, text)
	if len(hits) != 0 || n != 3 {
		t.Fatalf("hits = %v, comments = %d", hits, n)
	}
}

func TestScanFindsEveryKind(t *testing.T) {
	text := "<!-- outer\n  <!-- inner -->\n  still meant hidden\n-->\n" + // nested, then stray
		"a <!--> b <!---> c\n" + // two abrupt
		"tail <!-- closed on line 8\n```\n<!-- fence inside a comment is comment text -->\n"
	hits, n := scan(t, text)
	want := []hit{
		{2, 3, Nested},
		{4, 1, StrayClose},
		{5, 3, Abrupt},
		{5, 11, Abrupt},
		{8, 1, Nested}, // the fence line is comment text, the <!-- after it is not
	}
	if len(hits) != len(want) || n != 4 {
		t.Fatalf("hits = %v, comments = %d", hits, n)
	}
	for i, w := range want {
		if hits[i] != w {
			t.Errorf("hit %d = %v, want %v", i, hits[i], w)
		}
	}
	// The comment on line 6 closes on line 8, so nothing is unclosed; drop
	// the close and it is.
	hits, _ = scan(t, "x\ntail <!-- never closed\n")
	if len(hits) != 1 || hits[0] != (hit{2, 6, Unclosed}) {
		t.Fatalf("hits = %v", hits)
	}
}

func TestScanColumnsInCharacters(t *testing.T) {
	hits, _ := scan(t, "a\u00f1o --> x\n")
	if len(hits) != 1 || hits[0].col != 5 {
		t.Fatalf("hits = %v", hits)
	}
}

func TestScanUnmatchedBacktickIsText(t *testing.T) {
	hits, _ := scan(t, "a ` b --> c\n")
	if len(hits) != 1 || hits[0].kind != StrayClose {
		t.Fatalf("hits = %v", hits)
	}
}

func TestCheckDirectory(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.md", "<!-- ok -->\n")
	write("b.md", "x -->\n")
	write("c.txt", "--> not markdown, not read by default\n")
	res, err := Check(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Files != 2 || res.Comments != 1 || res.Found != 1 || res.Findings[0].Path != "b.md" {
		t.Fatalf("res = %+v", res)
	}
	res, err = Check(dir, Options{Include: []string{"*.txt"}})
	if err != nil || res.Found != 1 || res.Findings[0].Path != "c.txt" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	res, err = Check(dir, Options{Limit: 1, Include: []string{"*"}})
	if err != nil || res.Found != 2 || len(res.Findings) != 1 || !res.Truncated {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestCheckFails(t *testing.T) {
	empty := t.TempDir()
	if _, err := Check(empty, Options{}); err == nil || !strings.Contains(err.Error(), `"*.md" matches no file`) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Check(filepath.Join(empty, "none"), Options{}); err == nil {
		t.Fatal("missing path: no error")
	}
}
