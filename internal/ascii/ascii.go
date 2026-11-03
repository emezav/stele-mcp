// Package ascii lists every character outside ASCII in a file or the files
// under a directory, except those the caller allows, with its line, column,
// code point and Unicode name. It does not decide what is wrong: the allowed
// set is the caller's, because a project may write its docs with accents and
// its code in ASCII.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package ascii

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"unicode/utf8"

	"golang.org/x/text/unicode/runenames"

	"github.com/emezav/stele-mcp/internal/walk"
)

// DefaultLimit is how many findings come back when Options.Limit is not set.
const DefaultLimit = 100

// Spanish is what AllowSpanish adds: the precomposed letters and the opening
// marks of Spanish spelling (a e i o u with acute, u with diaeresis, n with
// tilde, both cases, and the inverted question and exclamation marks). A
// letter written as a base plus a combining accent (U+0301) is not in it and
// is reported.
const Spanish = "\u00e1\u00e9\u00ed\u00f3\u00fa\u00fc\u00f1\u00c1\u00c9\u00cd\u00d3\u00da\u00dc\u00d1\u00bf\u00a1"

// Options controls what is allowed and which files are read.
type Options struct {
	// Allow holds characters that are not reported, each one by itself.
	Allow string
	// AllowSpanish adds Spanish to Allow.
	AllowSpanish bool
	// Include and Exclude are base-name globs, as in path/filepath.Match.
	// Each one must match some file, or the check fails.
	Include []string
	Exclude []string
	// Limit caps Findings; <= 0 means DefaultLimit.
	Limit int
}

// Finding is one character outside ASCII and not allowed. Column counts
// characters from 1. For a byte that is not valid UTF-8, Char is empty, Code
// is the byte in hex and Name says so.
type Finding struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Char   string `json:"char"`
	Code   string `json:"code"`
	Name   string `json:"name"`
}

// Count is how many times one character was found, across all files.
type Count struct {
	Char  string `json:"char"`
	Code  string `json:"code"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Result is what the check found. Paths are relative to Root, with forward
// slashes. Found counts every finding; Findings holds at most Limit of them,
// and Truncated says so. Chars has one entry per distinct character, most
// frequent first. OK is true when Found is zero.
type Result struct {
	Root      string    `json:"root"`
	Files     int       `json:"files"`
	Allowed   string    `json:"allowed"`
	OK        bool      `json:"ok"`
	Found     int       `json:"found"`
	Truncated bool      `json:"truncated"`
	Findings  []Finding `json:"findings"`
	Chars     []Count   `json:"chars"`
	Binary    []string  `json:"binary"`
}

// Check reads root (a file or a directory, walked recursively without .git)
// and reports every character outside ASCII that is not allowed. Files with a
// NUL byte are binary: they are listed in Binary and not read. It fails,
// instead of reporting a clean result, when root does not exist, a glob
// matches no file, or no text file is left to read.
func Check(root string, o Options) (Result, error) {
	if root == "" {
		return Result{}, errors.New("path is required: the file or directory to check")
	}
	files, err := walk.Collect(root, o.Include, o.Exclude)
	if err != nil {
		return Result{}, err
	}

	allowed := o.Allow
	if o.AllowSpanish {
		allowed += Spanish
	}
	allow := map[rune]bool{}
	for _, r := range allowed {
		allow[r] = true
	}
	limit := o.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	res := Result{Root: root, Allowed: allowed, Findings: []Finding{}, Chars: []Count{}, Binary: []string{}}
	counts := map[string]*Count{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return Result{}, err
		}
		rel := walk.Relative(root, f)
		if bytes.IndexByte(data, 0) >= 0 {
			res.Binary = append(res.Binary, rel)
			continue
		}
		res.Files++
		scan(data, func(line, col int, fd Finding) {
			if r, _ := utf8.DecodeRuneInString(fd.Char); fd.Char != "" && allow[r] {
				return
			}
			fd.Path, fd.Line, fd.Column = rel, line, col
			res.Found++
			if len(res.Findings) < limit {
				res.Findings = append(res.Findings, fd)
			} else {
				res.Truncated = true
			}
			c := counts[fd.Code]
			if c == nil {
				c = &Count{Char: fd.Char, Code: fd.Code, Name: fd.Name}
				counts[fd.Code] = c
			}
			c.Count++
		})
	}
	if res.Files == 0 {
		return Result{}, fmt.Errorf("no text file to check under %s (%d binary)", root, len(res.Binary))
	}
	for _, c := range counts {
		res.Chars = append(res.Chars, *c)
	}
	sort.Slice(res.Chars, func(i, j int) bool {
		if res.Chars[i].Count != res.Chars[j].Count {
			return res.Chars[i].Count > res.Chars[j].Count
		}
		return res.Chars[i].Code < res.Chars[j].Code
	})
	res.OK = res.Found == 0
	return res, nil
}

// scan calls found for every rune at or above 0x80 and every invalid byte,
// with its line and column, both from 1.
func scan(data []byte, found func(line, col int, fd Finding)) {
	line, col := 1, 0
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		col++
		switch {
		case r == utf8.RuneError && size <= 1:
			found(line, col, Finding{Code: fmt.Sprintf("0x%02X", data[i]), Name: "invalid UTF-8 byte"})
		case r >= utf8.RuneSelf:
			found(line, col, Finding{Char: string(r), Code: fmt.Sprintf("U+%04X", r), Name: runenames.Name(r)})
		case r == '\n':
			line, col = line+1, 0
		}
		i += size
	}
}
