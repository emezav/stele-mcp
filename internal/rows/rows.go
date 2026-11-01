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
	if !isTableLine(lines[last]) {
		return Result{}, fmt.Errorf("the last line of %s is not a table row (line %d): the table must be the last content of the file", path, last+1)
	}
	start := last
	for start > 0 && isTableLine(lines[start-1]) {
		start--
	}
	if start+1 > last || !separatorRe.MatchString(strings.TrimSpace(lines[start+1])) {
		return Result{}, fmt.Errorf("the table ending %s has no header and separator (starts at line %d)", path, start+1)
	}
	header := lines[start]

	want := len(splitCells(header))
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

func isTableLine(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "|")
}

// splitCells splits a table line on unescaped pipes, dropping the outer ones.
func splitCells(line string) []string {
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
