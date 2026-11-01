// Package rows validates and appends rows to the Markdown table that closes
// an append-only file (session index, effort log, letter index).
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package rows

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Options controls how Append validates and writes.
type Options struct {
	// ASCIIOnly rejects any cell character outside 0x20-0x7E.
	ASCIIOnly bool
	// DryRun validates and builds the row without writing it.
	DryRun bool
}

// Result describes the row that was (or would be) written.
type Result struct {
	Line       string `json:"line"`
	LineNumber int    `json:"line_number"`
	Header     string `json:"header"`
	Written    bool   `json:"written"`
}

var separatorRe = regexp.MustCompile(`^\|( *:?-+:? *\|)+ *$`)

// Append checks cells against the header of the table that ends the file at
// path and, unless opts.DryRun, appends them as one row. The file must exist
// and the table must be its last content, with nothing after it, not even
// blank lines: a row appended there would fall outside the table.
func Append(path string, cells []string, opts Options) (Result, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("file does not exist: %s (append_row never creates files)", path)
	}
	if err != nil {
		return Result{}, err
	}
	content := string(data)

	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1] // content ended with a newline
	}
	if len(lines) == 0 {
		return Result{}, fmt.Errorf("%s is empty: no table to append to", path)
	}

	last := len(lines) - 1
	if strings.TrimSpace(lines[last]) == "" {
		return Result{}, fmt.Errorf("%s ends with a blank line (line %d): a row appended there would fall outside the table", path, last+1)
	}
	if !IsTableLine(lines[last]) {
		return Result{}, fmt.Errorf("the last line of %s is not a table row (line %d): the table must be the last content of the file", path, last+1)
	}
	start, err := TableStart(lines, last)
	if err != nil {
		return Result{}, fmt.Errorf("the table ending %s %w", path, err)
	}
	header := lines[start]

	want := len(SplitCells(header))
	if len(cells) != want {
		return Result{}, fmt.Errorf("column count: header has %d, row has %d", want, len(cells))
	}
	for i, c := range cells {
		if err := checkCell(c, opts.ASCIIOnly); err != nil {
			return Result{}, fmt.Errorf("cell %d: %w", i+1, err)
		}
	}

	line := "| " + strings.Join(cells, " | ") + " |"
	res := Result{Line: line, LineNumber: len(lines) + 1, Header: header}
	if opts.DryRun {
		return res, nil
	}

	var buf bytes.Buffer
	if !strings.HasSuffix(content, "\n") {
		buf.WriteString(eol)
	}
	buf.WriteString(line)
	buf.WriteString(eol)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return Result{}, err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return Result{}, err
	}
	if err := f.Close(); err != nil {
		return Result{}, err
	}
	res.Written = true
	return res, nil
}

// ReadClosingTable reads the file at path and locates the table that closes
// it, allowing trailing blank lines; any other content after the table is an
// error. It returns the lines, the index of the header line and the index of
// the last row.
func ReadClosingTable(path string) (lines []string, start, last int, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, 0, fmt.Errorf("file does not exist: %s", path)
	}
	if err != nil {
		return nil, 0, 0, err
	}
	lines = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	last = len(lines) - 1
	for last >= 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last < 0 {
		return nil, 0, 0, fmt.Errorf("%s is empty: no table", path)
	}
	if !IsTableLine(lines[last]) {
		return nil, 0, 0, fmt.Errorf("the last content of %s is not a table row (line %d): the table must close the file", path, last+1)
	}
	start, err = TableStart(lines, last)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("the table ending %s %w", path, err)
	}
	return lines, start, last, nil
}

// FindColumn returns the index of the header cell named name, ignoring case
// and surrounding spaces. A name that is missing or appears twice is an error
// that lists the header.
func FindColumn(header []string, name string) (int, error) {
	want := strings.TrimSpace(name)
	found := -1
	names := make([]string, len(header))
	for i, h := range header {
		names[i] = strings.TrimSpace(h)
		if strings.EqualFold(names[i], want) {
			if found >= 0 {
				return 0, fmt.Errorf("column %q appears twice in the header", want)
			}
			found = i
		}
	}
	if found < 0 {
		return 0, fmt.Errorf("no column %q; the header has: %s", want, strings.Join(names, ", "))
	}
	return found, nil
}

// TableStart walks back from lines[last], a table row, to the first line of
// its table and returns that index. The table must open with a header and a
// separator line.
func TableStart(lines []string, last int) (int, error) {
	start := last
	for start > 0 && IsTableLine(lines[start-1]) {
		start--
	}
	if start+1 > last || !separatorRe.MatchString(strings.TrimSpace(lines[start+1])) {
		return 0, fmt.Errorf("has no header and separator (starts at line %d)", start+1)
	}
	return start, nil
}

// IsTableLine reports whether s is a line of a Markdown table.
func IsTableLine(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "|")
}

// SplitCells splits a table line on unescaped pipes, dropping the outer ones.
func SplitCells(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var cells []string
	cur := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '|' && (i == 0 || s[i-1] != '\\') {
			cells = append(cells, s[cur:i])
			cur = i + 1
		}
	}
	return append(cells, s[cur:])
}

func checkCell(c string, asciiOnly bool) error {
	if strings.ContainsAny(c, "\r\n") {
		return errors.New("contains a line break")
	}
	for i := 0; i < len(c); i++ {
		if c[i] == '|' && (i == 0 || c[i-1] != '\\') {
			return fmt.Errorf("unescaped '|' at byte %d (write it as \\|)", i)
		}
	}
	if asciiOnly {
		for _, r := range c {
			if r < 0x20 || r > 0x7E {
				return fmt.Errorf("non-ASCII character U+%04X", r)
			}
		}
	}
	return nil
}
