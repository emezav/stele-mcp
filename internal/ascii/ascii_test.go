package ascii

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setup writes the named files, with their content, into a fresh folder.
func setup(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheckReportsWhereAndWhat(t *testing.T) {
	dir := setup(t, map[string]string{
		"a.md":       "plain\nuna canci\u00f3n \u2014 fin\n",
		"sub/b.go":   "// x \u2192 y\n",
		"clean.txt":  "only ascii\r\n",
		".git/c.txt": "\u2014 never read\n",
	})
	res, err := Check(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Files != 3 || res.Found != 3 || res.Truncated {
		t.Fatalf("res = %+v", res)
	}
	want := []Finding{
		{Path: "a.md", Line: 2, Column: 10, Char: "\u00f3", Code: "U+00F3", Name: "LATIN SMALL LETTER O WITH ACUTE"},
		{Path: "a.md", Line: 2, Column: 13, Char: "\u2014", Code: "U+2014", Name: "EM DASH"},
		{Path: "sub/b.go", Line: 1, Column: 6, Char: "\u2192", Code: "U+2192", Name: "RIGHTWARDS ARROW"},
	}
	for i, w := range want {
		if res.Findings[i] != w {
			t.Errorf("finding %d = %+v, want %+v", i, res.Findings[i], w)
		}
	}
	if len(res.Chars) != 3 || res.Chars[0].Count != 1 {
		t.Errorf("chars = %+v", res.Chars)
	}
}

func TestCheckAllowSpanish(t *testing.T) {
	dir := setup(t, map[string]string{
		"a.md": "\u00bfQu\u00e9 a\u00f1o? \u00a1S\u00ed! \u2014 a\u0301\n",
	})
	res, err := Check(dir, Options{AllowSpanish: true})
	if err != nil {
		t.Fatal(err)
	}
	// The em dash and the combining accent, which looks like a precomposed letter and is not.
	if res.Found != 2 || res.Findings[0].Code != "U+2014" || res.Findings[1].Code != "U+0301" {
		t.Fatalf("res = %+v", res)
	}
	if res.Allowed != Spanish {
		t.Errorf("allowed = %q", res.Allowed)
	}

	res, err = Check(dir, Options{AllowSpanish: true, Allow: "\u2014\u0301"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Findings == nil || res.Chars == nil {
		t.Fatalf("res = %+v", res)
	}
}

func TestCheckInvalidAndBinary(t *testing.T) {
	dir := setup(t, map[string]string{
		"latin1.txt": "caf\xe9\n",
		"img.png":    "\x89PNG\x00\x01",
	})
	res, err := Check(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	f := res.Findings[0]
	if res.Files != 1 || res.Found != 1 || f.Char != "" || f.Code != "0xE9" || f.Column != 4 {
		t.Fatalf("res = %+v", res)
	}
	if len(res.Binary) != 1 || res.Binary[0] != "img.png" {
		t.Errorf("binary = %v", res.Binary)
	}
}

func TestCheckLimit(t *testing.T) {
	dir := setup(t, map[string]string{"a.md": strings.Repeat("\u2014", 5)})
	res, err := Check(dir, Options{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Found != 5 || len(res.Findings) != 2 || !res.Truncated || res.Chars[0].Count != 5 {
		t.Fatalf("res = %+v", res)
	}
}

func TestCheckSingleFileAndGlobs(t *testing.T) {
	dir := setup(t, map[string]string{"a.md": "\u2014\n", "b.go": "ok\n"})
	res, err := Check(filepath.Join(dir, "a.md"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 || res.Findings[0].Path != filepath.ToSlash(filepath.Join(dir, "a.md")) {
		t.Fatalf("res = %+v", res)
	}
	res, err = Check(dir, Options{Include: []string{"*.go"}})
	if err != nil || !res.OK || res.Files != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestCheckRefusals(t *testing.T) {
	dir := setup(t, map[string]string{"a.md": "x\n", "img.bin": "\x00"})
	cases := []struct {
		name string
		root string
		o    Options
		want string
	}{
		{"no path", "", Options{}, "path is required"},
		{"missing", filepath.Join(dir, "nope"), Options{}, ""},
		{"dead include", dir, Options{Include: []string{"*.go"}}, `glob "*.go" matches no file`},
		{"dead exclude", dir, Options{Exclude: []string{"*.txt"}}, `glob "*.txt" matches no file`},
		{"bad glob", dir, Options{Include: []string{"["}}, "bad glob"},
		{"nothing left", dir, Options{Exclude: []string{"*.md"}}, "no text file to check"},
		{"empty dir", t.TempDir(), Options{}, "no text file to check"},
	}
	for _, c := range cases {
		_, err := Check(c.root, c.o)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}
