package rows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const index = "# index\n\nintro\n\n| N | Date | Summary |\n| --- | --- | --- |\n| 1 | 2026-11-01 | first |\n"

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAppendWritesRowVerbatim(t *testing.T) {
	p := write(t, index)
	res, err := Append(p, []string{"2", "2026-11-02", `95% clean \| 100%s`}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `| 2 | 2026-11-02 | 95% clean \| 100%s |`
	if res.Line != want || res.LineNumber != 8 || !res.Written || res.Header != "| N | Date | Summary |" {
		t.Fatalf("result = %+v", res)
	}
	if got := read(t, p); got != index+want+"\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestAppendKeepsCRLFAndAddsMissingNewline(t *testing.T) {
	p := write(t, strings.ReplaceAll(strings.TrimSuffix(index, "\n"), "\n", "\r\n"))
	if _, err := Append(p, []string{"2", "d", "s"}, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); !strings.HasSuffix(got, "| 1 | 2026-11-01 | first |\r\n| 2 | d | s |\r\n") {
		t.Fatalf("file = %q", got)
	}
}

func TestAppendDryRunDoesNotWrite(t *testing.T) {
	p := write(t, index)
	res, err := Append(p, []string{"2", "d", "s"}, Options{DryRun: true})
	if err != nil || res.Written {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if read(t, p) != index {
		t.Fatal("dry run modified the file")
	}
}

func TestAppendRejects(t *testing.T) {
	cases := []struct {
		name    string
		content string
		cells   []string
		opts    Options
		want    string
	}{
		{"column count", index, []string{"2", "d"}, Options{}, "header has 3, row has 2"},
		{"unescaped pipe", index, []string{"2", "d", "a | b"}, Options{}, "unescaped '|'"},
		{"line break", index, []string{"2", "d", "a\nb"}, Options{}, "line break"},
		{"non-ASCII", index, []string{"2", "d", "sesi\u00f3n"}, Options{ASCIIOnly: true}, "U+00F3"},
		{"blank after table", index + "\n", []string{"2", "d", "s"}, Options{}, "blank line"},
		{"text after table", index + "\nnote\n", []string{"2", "d", "s"}, Options{}, "not a table row"},
		{"no separator", "| a | b |\n| 1 | 2 |\n", []string{"x", "y"}, Options{}, "no header and separator"},
		{"empty", "", []string{"x"}, Options{}, "empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := write(t, c.content)
			_, err := Append(p, c.cells, c.opts)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if read(t, p) != c.content {
				t.Fatal("rejected append modified the file")
			}
		})
	}
}

func TestAppendAcceptsNonASCIIByDefault(t *testing.T) {
	p := write(t, index)
	if _, err := Append(p, []string{"2", "d", "sesi\u00f3n"}, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestAppendMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.md")
	if _, err := Append(p, []string{"x"}, Options{}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("append_row created the file")
	}
}

func TestHeaderWithEscapedPipe(t *testing.T) {
	p := write(t, "| a \\| b | c |\n| --- | --- |\n")
	if _, err := Append(p, []string{"1", "2"}, Options{}); err != nil {
		t.Fatal(err)
	}
}
