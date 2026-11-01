package sum

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const effort = "# effort\n\n| Session | Date | hours | Summary |\n| --- | --- | --- | --- |\n" +
	"| 1 | 2026-11-01 | 3.25-4 | first, 95% |\n" +
	"| 2 | 2026-11-01 | 2 | a single value \\| escaped pipe |\n" +
	"| 3 | 2026-11-02 | 1,5 - 2,5 | decimal comma |\n"

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestColumnSumsSinglesAndRanges(t *testing.T) {
	res, err := Column(write(t, effort), "Hours")
	if err != nil {
		t.Fatal(err)
	}
	want := Result{Column: "hours", Header: "| Session | Date | hours | Summary |",
		Rows: 3, Singles: 1, Ranges: 2, Low: 6.75, High: 8.5, Mid: 7.625}
	if res != want {
		t.Fatalf("res = %+v, want %+v", res, want)
	}
}

func TestColumnDoesNotDrift(t *testing.T) {
	// 0.1 + 0.2 in float64 is 0.30000000000000004.
	res, err := Column(write(t, "| n | h |\n| --- | --- |\n| 1 | 0.1 |\n| 2 | 0.2 |\n"), "h")
	if err != nil || res.Low != 0.3 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestColumnAcceptsCRLFAndTrailingBlankLines(t *testing.T) {
	res, err := Column(write(t, strings.ReplaceAll(effort, "\n", "\r\n")+"\r\n\r\n"), "hours")
	if err != nil || res.Rows != 3 || res.High != 8.5 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestColumnEmptyTable(t *testing.T) {
	res, err := Column(write(t, "| n | h |\n| --- | --- |\n"), "h")
	if err != nil || res.Rows != 0 || res.Mid != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestColumnRejects(t *testing.T) {
	cases := []struct {
		name    string
		content string
		column  string
		want    string
	}{
		{"unknown column", effort, "minutes", "the header has: Session, Date, hours, Summary"},
		{"duplicate column", "| h | H |\n| --- | --- |\n", "h", "appears twice"},
		{"not a number", effort + "| 4 | d | ~3 | s |\n", "hours", `1 of 4 rows cannot be summed in column "hours", so nothing was summed: line 8 "~3"`},
		{"empty cell", effort + "| 4 | d |  | s |\n", "hours", `line 8 "": empty`},
		{"backwards range", effort + "| 4 | d | 5-3 | s |\n", "hours", "backwards"},
		{"cell count", effort + "| 4 | d | 3 | s | extra |\n", "hours", "line 8 has 5 cells, header has 4"},
		{"text after table", effort + "\nnote\n", "hours", "not a table row"},
		{"no separator", "| n | h |\n| 1 | 2 |\n", "h", "no header and separator"},
		{"empty", "\n\n", "h", "empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Column(write(t, c.content), c.column)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestColumnListsAtMostTenBadRows(t *testing.T) {
	content := "| n | h |\n| --- | --- |\n" + strings.Repeat("| 1 | x |\n", 12)
	_, err := Column(write(t, content), "h")
	if err == nil || !strings.Contains(err.Error(), "; and 2 more") || strings.Count(err.Error(), `"x"`) != 10 {
		t.Fatalf("err = %v", err)
	}
}

func TestColumnMissingFile(t *testing.T) {
	_, err := Column(filepath.Join(t.TempDir(), "none.md"), "h")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v", err)
	}
}
