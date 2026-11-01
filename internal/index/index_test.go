package index

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const letters = "# letters\n\n| # | Date | Dir | Subject |\n| --- | --- | --- | --- |\n" +
	"| 001 | 2026-11-01 | -> | first, 95% |\n" +
	"| 002 | 2026-11-01 | <- | escaped \\| pipe |\n" +
	"| 003 | 2026-11-02 | -> | third |\n"

// setup writes the index and the named files into a fresh folder.
func setup(t *testing.T, content string, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "letters.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestCheckAllMatch(t *testing.T) {
	p := setup(t, letters, "letter-001-2026-11-01.md", "letter-002-2026-11-01.md", "letter-003-2026-11-02.md", "other-009-2026-11-01.md")
	res, err := Check(p, "letter", Options{DateColumn: "date"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Rows != 3 || res.Files != 3 || res.Dir != filepath.Dir(p) {
		t.Fatalf("res = %+v", res)
	}
	if res.MissingFiles == nil || res.Gaps == nil || res.DateMismatches == nil {
		t.Fatalf("empty lists must not be nil: %+v", res)
	}
}

func TestCheckReportsEveryKind(t *testing.T) {
	content := letters + "| 3 | 2026-11-02 | -> | repeated, unpadded |\n| 006 | 2026-11-03 | -> | no file |\n"
	p := setup(t, content,
		"letter-001-2026-10-31.md",                            // date differs from its row
		"letter-002-2026-11-01.md", "letter-02-2026-11-05.md", // two files for 2
		"letter-003-2026-11-02.md",
		"letter-007-2026-11-04.md", // no row
		"letter-008.md",            // malformed
	)
	res, err := Check(p, "letter", Options{DateColumn: "Date"})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Rows != 5 || res.Files != 5 {
		t.Fatalf("res = %+v", res)
	}
	check := func(name string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("missing", res.MissingFiles, []Row{{6, 9}})
	check("unindexed", res.Unindexed, []string{"letter-007-2026-11-04.md"})
	check("duplicate rows", res.DuplicateRows, []Row{{3, 7}, {3, 8}})
	check("duplicate files", res.DuplicateFiles, []string{"letter-002-2026-11-01.md", "letter-02-2026-11-05.md"})
	check("dates", res.DateMismatches, []DateMismatch{{1, 5, "2026-11-01", "letter-001-2026-10-31.md"}})
	check("malformed", res.Malformed, []string{"letter-008.md"})
	check("gaps", res.Gaps, []int{4, 5})
}

func TestCheckWithoutDateColumnSkipsDates(t *testing.T) {
	p := setup(t, letters, "letter-001-2020-01-01.md", "letter-002-2026-11-01.md", "letter-003-2026-11-02.md")
	res, err := Check(p, "letter", Options{})
	if err != nil || !res.OK {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestCheckNumberColumnAndDir(t *testing.T) {
	content := "| Date | Session |\n| --- | --- |\n| 2026-11-01 | 1 |\n\n"
	p := setup(t, content)
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "session-001-2026-11-01.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Check(p, "session", Options{Dir: other, NumberColumn: "session", DateColumn: "date"})
	if err != nil || !res.OK || res.Rows != 1 || res.Files != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestCheckRejects(t *testing.T) {
	cases := []struct {
		name    string
		content string
		prefix  string
		opts    Options
		want    string
	}{
		{"wrong prefix", letters, "carta", Options{}, "no file named carta-* in"},
		{"empty prefix", letters, " ", Options{}, "prefix is empty"},
		{"not a number", letters + "| 4a | d | -> | s |\n", "letter", Options{}, `1 of 4 rows have no number in column "#", so nothing was checked: line 8 "4a"`},
		{"cell count", letters + "| 4 | d | -> |\n", "letter", Options{}, "line 8 has 3 cells, header has 4"},
		{"unknown column", letters, "letter", Options{DateColumn: "fecha"}, "the header has: #, Date, Dir, Subject"},
		{"text after table", letters + "\nnote\n", "letter", Options{}, "not a table row"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Check(setup(t, c.content, "letter-001-2026-11-01.md"), c.prefix, c.opts)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestCheckMissingDir(t *testing.T) {
	_, err := Check(setup(t, letters), "letter", Options{Dir: filepath.Join(t.TempDir(), "none")})
	if err == nil {
		t.Fatal("want an error for a missing folder")
	}
}
