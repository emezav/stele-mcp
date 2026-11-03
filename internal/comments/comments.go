// Package comments finds the HTML comments in Markdown files that do not do
// what they look like: a <!-- inside a comment (comments do not nest, so the
// first --> closes both and what follows is live), a --> outside any comment,
// a comment never closed, and the abrupt forms <!--> and <!--->, which are
// complete empty comments. A broken comment does not show when the source is
// reread; it shows when the Markdown is rendered.
//
// Fenced code blocks and inline code spans are not read, so a comment marker
// written as an example in backticks is not a finding.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package comments

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/emezav/stele-mcp/internal/walk"
)

// DefaultLimit is how many findings come back when Options.Limit is not set.
const DefaultLimit = 100

// DefaultInclude is the include glob used when a directory is checked and the
// caller gives none.
const DefaultInclude = "*.md"

// Finding kinds.
const (
	Nested     = "nested"
	StrayClose = "stray_close"
	Unclosed   = "unclosed"
	Abrupt     = "abrupt"
)

// Options controls which files are read.
type Options struct {
	// Include and Exclude are base-name globs, as in path/filepath.Match.
	// Each one must match some file, or the check fails. For a directory,
	// an empty Include means DefaultInclude.
	Include []string
	Exclude []string
	// Limit caps Findings; <= 0 means DefaultLimit.
	Limit int
}

// Finding is one comment marker that does not do what it looks like. Column
// counts characters from 1.
type Finding struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// Result is what the check found. Paths are relative to Root, with forward
// slashes. Comments counts the comments that open and close; Found counts
// every finding, Findings holds at most Limit of them. OK is true when Found
// is zero.
type Result struct {
	Root      string    `json:"root"`
	Files     int       `json:"files"`
	Comments  int       `json:"comments"`
	OK        bool      `json:"ok"`
	Found     int       `json:"found"`
	Truncated bool      `json:"truncated"`
	Findings  []Finding `json:"findings"`
	Binary    []string  `json:"binary"`
}

// Check reads root (a Markdown file, or a directory walked recursively
// without .git) and reports every comment marker that does not do what it
// looks like. It fails, instead of reporting a clean result, when root does
// not exist, a glob matches no file, or no text file is left to read.
func Check(root string, o Options) (Result, error) {
	if root == "" {
		return Result{}, errors.New("path is required: the file or directory to check")
	}
	include := o.Include
	if info, err := os.Stat(root); err == nil && info.IsDir() && len(include) == 0 {
		include = []string{DefaultInclude}
	}
	files, err := walk.Collect(root, include, o.Exclude)
	if err != nil {
		return Result{}, err
	}
	limit := o.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	res := Result{Root: root, Findings: []Finding{}, Binary: []string{}}
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
		res.Comments += Scan(string(data), func(fd Finding) {
			fd.Path = rel
			res.Found++
			if len(res.Findings) < limit {
				res.Findings = append(res.Findings, fd)
			} else {
				res.Truncated = true
			}
		})
	}
	if res.Files == 0 {
		return Result{}, fmt.Errorf("no text file to check under %s (%d binary)", root, len(res.Binary))
	}
	res.OK = res.Found == 0
	return res, nil
}

// Scan reads one Markdown text, calls found for every finding (without its
// path) and returns how many comments open and close. Comments run until the
// first -->, across lines, as in HTML and CommonMark.
func Scan(text string, found func(Finding)) int {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	comments := 0
	inComment := false
	openLine, openCol := 0, 0
	fence := ""
	for i, line := range lines {
		n := i + 1
		col := func(pos int) int { return utf8.RuneCountInString(line[:pos]) + 1 }
		if !inComment {
			if fence != "" {
				if isFenceClose(line, fence) {
					fence = ""
				}
				continue
			}
			if f := fenceOpen(line); f != "" {
				fence = f
				continue
			}
		}
		pos := 0
		for pos < len(line) {
			if inComment {
				end := strings.Index(line[pos:], "-->")
				stop := len(line)
				if end >= 0 {
					stop = pos + end
				}
				for _, o := range indexAll(line[pos:stop], "<!--") {
					found(Finding{Line: n, Column: col(pos + o), Kind: Nested,
						Detail: fmt.Sprintf("<!-- inside the comment opened at line %d column %d; comments do not nest, so the first --> closes both", openLine, openCol)})
				}
				if end < 0 {
					pos = len(line)
					break
				}
				inComment = false
				comments++
				pos = stop + 3
				continue
			}
			next, what := nextMarker(line, pos)
			if next < 0 {
				break
			}
			switch what {
			case "`":
				pos = skipCodeSpan(line, next)
			case "-->":
				found(Finding{Line: n, Column: col(next), Kind: StrayClose,
					Detail: "--> outside any comment: the text before it, meant as comment, is live"})
				pos = next + 3
			case "<!--":
				rest := line[next+4:]
				if strings.HasPrefix(rest, ">") || strings.HasPrefix(rest, "->") {
					form := "<!-->"
					if rest[0] == '-' {
						form = "<!--->"
					}
					found(Finding{Line: n, Column: col(next), Kind: Abrupt,
						Detail: form + " is a complete empty comment: what follows it is live"})
					comments++
					pos = next + len(form)
					continue
				}
				inComment = true
				openLine, openCol = n, col(next)
				pos = next + 4
			}
		}
	}
	if inComment {
		found(Finding{Line: openLine, Column: openCol, Kind: Unclosed,
			Detail: "comment never closed: everything after it is hidden"})
	}
	return comments
}

// nextMarker returns the position and kind of the first backtick, <!-- or -->
// at or after pos, or -1.
func nextMarker(line string, pos int) (int, string) {
	best, what := -1, ""
	for _, m := range []string{"`", "<!--", "-->"} {
		if i := strings.Index(line[pos:], m); i >= 0 && (best < 0 || pos+i < best) {
			best, what = pos+i, m
		}
	}
	return best, what
}

// skipCodeSpan returns the position after the code span that opens with the
// backtick run at pos, or after the run itself if no run of the same length
// closes it on this line.
func skipCodeSpan(line string, pos int) int {
	n := 0
	for pos+n < len(line) && line[pos+n] == '`' {
		n++
	}
	run := strings.Repeat("`", n)
	for i := pos + n; i < len(line); {
		j := strings.Index(line[i:], run)
		if j < 0 {
			break
		}
		k := i + j
		m := 0
		for k+m < len(line) && line[k+m] == '`' {
			m++
		}
		if m == n {
			return k + n
		}
		i = k + m
	}
	return pos + n
}

// fenceOpen returns the fence marker (``` or ~~~, possibly longer) that opens
// a fenced code block on this line, or "".
func fenceOpen(line string) string {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return ""
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 || (t[0] == '`' && strings.Contains(t[n:], "`")) {
		return ""
	}
	return t[:n]
}

// isFenceClose reports whether line closes a block opened with fence: the
// same character, at least as long, and nothing after it but spaces.
func isFenceClose(line, fence string) bool {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return false
	}
	n := 0
	for n < len(t) && t[n] == fence[0] {
		n++
	}
	return n >= len(fence) && strings.TrimSpace(t[n:]) == ""
}

func indexAll(s, sub string) []int {
	var out []int
	for i := 0; ; {
		j := strings.Index(s[i:], sub)
		if j < 0 {
			return out
		}
		out = append(out, i+j)
		i += j + len(sub)
	}
}
