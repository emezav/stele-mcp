// Package index cross-checks an append-only index, such as the session index
// or the letter index, against the files it lists: every number in the index
// should have one file <prefix>-<number>-<YYYY-MM-DD>.md in its folder, and
// every such file one row. It reports what does not match instead of deciding
// whether that is a defect: a row without a file can be a documented absence.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package index

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/emezav/stele-mcp/internal/rows"
)

// Options controls which columns are read and where the files are.
type Options struct {
	// Dir is the folder holding the files; empty means the index's folder.
	Dir string
	// NumberColumn is the header of the number column; empty means the first.
	NumberColumn string
	// DateColumn is the header of a date column to compare with the date in
	// each file name; empty skips the comparison.
	DateColumn string
}

// Row is a row of the index, by its number and line.
type Row struct {
	Number int `json:"number"`
	Line   int `json:"line"`
}

// DateMismatch is a row whose date differs from the date in its file name.
type DateMismatch struct {
	Number  int    `json:"number"`
	Line    int    `json:"line"`
	RowDate string `json:"row_date"`
	File    string `json:"file"`
}

// Result is what the cross-check found. OK is true when every list is empty.
type Result struct {
	Header         string         `json:"header"`
	Dir            string         `json:"dir"`
	Rows           int            `json:"rows"`
	Files          int            `json:"files"`
	OK             bool           `json:"ok"`
	MissingFiles   []Row          `json:"missing_files"`
	Unindexed      []string       `json:"unindexed"`
	DuplicateRows  []Row          `json:"duplicate_rows"`
	DuplicateFiles []string       `json:"duplicate_files"`
	DateMismatches []DateMismatch `json:"date_mismatches"`
	Malformed      []string       `json:"malformed"`
	Gaps           []int          `json:"gaps"`
}

// maxListed caps how many offending rows an error names.
const maxListed = 10

var numberRe = regexp.MustCompile(`^\d+$`)

// Check reads the table that ends the index at path and the files named
// prefix-* in the folder, and reports rows without a file, files without a
// row, repeated numbers, file names that do not have the expected shape,
// numbers missing from both, and, with DateColumn, dates that differ.
// A number cell that is not a number fails the whole check, and so does a
// folder with no file carrying the prefix, which is a wrong prefix or folder
// far more often than an empty index.
func Check(path, prefix string, opts Options) (Result, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return Result{}, fmt.Errorf("prefix is empty: give the file name prefix, such as sesion or carta")
	}
	lines, start, last, err := rows.ReadClosingTable(path)
	if err != nil {
		return Result{}, err
	}
	header := rows.SplitCells(lines[start])
	numCol := 0
	if opts.NumberColumn != "" {
		if numCol, err = rows.FindColumn(header, opts.NumberColumn); err != nil {
			return Result{}, err
		}
	}
	dateCol := -1
	if opts.DateColumn != "" {
		if dateCol, err = rows.FindColumn(header, opts.DateColumn); err != nil {
			return Result{}, err
		}
	}

	dir := opts.Dir
	if dir == "" {
		dir = filepath.Dir(path)
	}
	res := Result{Header: lines[start], Dir: dir}

	byNumber := map[int][]Row{}
	dates := map[int][]string{}
	var bad []string
	for i := start + 2; i <= last; i++ {
		res.Rows++
		cells := rows.SplitCells(lines[i])
		if len(cells) != len(header) {
			bad = append(bad, fmt.Sprintf("line %d has %d cells, header has %d", i+1, len(cells), len(header)))
			continue
		}
		cell := strings.TrimSpace(cells[numCol])
		if !numberRe.MatchString(cell) {
			bad = append(bad, fmt.Sprintf("line %d %q is not a number", i+1, cell))
			continue
		}
		n, _ := strconv.Atoi(cell)
		byNumber[n] = append(byNumber[n], Row{Number: n, Line: i + 1})
		if dateCol >= 0 {
			dates[n] = append(dates[n], strings.TrimSpace(cells[dateCol]))
		}
	}
	if len(bad) > 0 {
		listed := bad
		if len(listed) > maxListed {
			listed = listed[:maxListed]
		}
		more := ""
		if len(bad) > maxListed {
			more = fmt.Sprintf("; and %d more", len(bad)-maxListed)
		}
		return Result{}, fmt.Errorf("%d of %d rows have no number in column %q, so nothing was checked: %s%s",
			len(bad), res.Rows, strings.TrimSpace(header[numCol]), strings.Join(listed, "; "), more)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return Result{}, err
	}
	nameRe := regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + `-(\d+)-(\d{4}-\d{2}-\d{2})\.md$`)
	files := map[int][]string{}
	fileDates := map[string]string{}
	carrying := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix+"-") {
			continue
		}
		carrying++
		m := nameRe.FindStringSubmatch(name)
		if m == nil {
			res.Malformed = append(res.Malformed, name)
			continue
		}
		n, _ := strconv.Atoi(m[1])
		files[n] = append(files[n], name)
		fileDates[name] = m[2]
		res.Files++
	}
	if carrying == 0 {
		return Result{}, fmt.Errorf("no file named %s-* in %s: check prefix and dir", prefix, dir)
	}

	seen := map[int]bool{}
	for n, rs := range byNumber {
		seen[n] = true
		if len(rs) > 1 {
			res.DuplicateRows = append(res.DuplicateRows, rs...)
		}
		fs := files[n]
		if len(fs) == 0 {
			res.MissingFiles = append(res.MissingFiles, rs...)
			continue
		}
		if dateCol < 0 || len(fs) > 1 {
			continue
		}
		for j, r := range rs {
			if d := dates[n][j]; d != fileDates[fs[0]] {
				res.DateMismatches = append(res.DateMismatches, DateMismatch{Number: n, Line: r.Line, RowDate: d, File: fs[0]})
			}
		}
	}
	for n, fs := range files {
		seen[n] = true
		if len(fs) > 1 {
			res.DuplicateFiles = append(res.DuplicateFiles, fs...)
		}
		if len(byNumber[n]) == 0 {
			res.Unindexed = append(res.Unindexed, fs...)
		}
	}
	if len(seen) > 0 {
		lo, hi := -1, -1
		for n := range seen {
			if lo < 0 || n < lo {
				lo = n
			}
			if n > hi {
				hi = n
			}
		}
		for n := lo + 1; n < hi; n++ {
			if !seen[n] {
				res.Gaps = append(res.Gaps, n)
			}
		}
	}

	sortRows(res.MissingFiles)
	sortRows(res.DuplicateRows)
	sort.Strings(res.Unindexed)
	sort.Strings(res.DuplicateFiles)
	sort.Strings(res.Malformed)
	sort.Slice(res.DateMismatches, func(i, j int) bool { return res.DateMismatches[i].Line < res.DateMismatches[j].Line })

	res.OK = len(res.MissingFiles)+len(res.Unindexed)+len(res.DuplicateRows)+len(res.DuplicateFiles)+
		len(res.DateMismatches)+len(res.Malformed)+len(res.Gaps) == 0
	res.MissingFiles = nonNil(res.MissingFiles)
	res.Unindexed = nonNil(res.Unindexed)
	res.DuplicateRows = nonNil(res.DuplicateRows)
	res.DuplicateFiles = nonNil(res.DuplicateFiles)
	res.DateMismatches = nonNil(res.DateMismatches)
	res.Malformed = nonNil(res.Malformed)
	res.Gaps = nonNil(res.Gaps)
	return res, nil
}

func sortRows(rs []Row) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].Line < rs[j].Line })
}

// nonNil keeps an empty list as [] in JSON instead of null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
