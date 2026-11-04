// Package next composes the session record a stele close is about to write:
// the next session number, the file name from the manifest's pattern, and
// the paths the close appends to, because an append to a mistyped path
// creates the file it was meant to find, and nothing fails. It also checks
// that the index and the history folder end at the same number: when they do
// not, a close was left half done.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package next

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emezav/stele-mcp/internal/clock"
	"github.com/emezav/stele-mcp/internal/manifest"
	"github.com/emezav/stele-mcp/internal/rows"
)

// Options controls where the manifest is and which day it is.
type Options struct {
	// Config is the manifest path; empty means stele.config.md under root.
	Config string
	// Today is the date YYYY-MM-DD; empty means the machine's local date,
	// with the time zone probe of the today tool.
	Today string
}

// Result is the next session record and the paths around it. Paths are
// relative to the project root, with forward slashes. OK is true when the
// index and the history folder end at the same number.
type Result struct {
	Number      int      `json:"number"`
	Padded      string   `json:"padded"`
	Date        string   `json:"date"`
	DateVerdict string   `json:"date_verdict"`
	File        string   `json:"file"`
	Path        string   `json:"path"`
	LastFiles   []string `json:"last_files"`
	Index       string   `json:"index"`
	IndexLast   int      `json:"index_last"`
	FilesLast   int      `json:"files_last"`
	Effort      string   `json:"effort"`
	OK          bool     `json:"ok"`
	Advice      string   `json:"advice"`
	Pattern     string   `json:"pattern"`
	HistoryDir  string   `json:"history_dir"`
	DateAdvice  string   `json:"date_advice"`
}

var tokenRe = regexp.MustCompile(`\{N+\}`)

// Check reads the manifest of the instance at root, the session index and
// the history folder, and composes the next session record. It fails if the
// pattern has no number or date token, the index cannot be read, or the
// date is malformed.
func Check(root string, o Options) (Result, error) {
	if root == "" {
		root = "."
	}
	cfg := o.Config
	if cfg == "" {
		cfg = filepath.Join(root, manifest.DefaultName)
	}
	m, err := manifest.Read(cfg)
	if err != nil {
		return Result{}, err
	}
	pattern, err := m.Name("session")
	if err != nil {
		return Result{}, fmt.Errorf("session: %w", err)
	}
	tok := tokenRe.FindString(pattern)
	if tok == "" || !strings.Contains(pattern, "{YYYY-MM-DD}") {
		return Result{}, fmt.Errorf("session pattern %q needs a {NNN} and a {YYYY-MM-DD} token", pattern)
	}
	width := len(tok) - 2
	hist, err := m.ContainerPath("history_dir")
	if err != nil {
		return Result{}, fmt.Errorf("history_dir: %w", err)
	}
	idx, err := m.RolePath("index")
	if err != nil {
		return Result{}, fmt.Errorf("index: %w", err)
	}
	res := Result{Pattern: pattern, HistoryDir: hist, Index: idx}

	if res.IndexLast, err = lastNumber(filepath.Join(m.Root, filepath.FromSlash(idx))); err != nil {
		return Result{}, err
	}
	fileRe := regexp.MustCompile("^" + strings.NewReplacer(
		regexp.QuoteMeta(tok), `(\d+)`,
		regexp.QuoteMeta("{YYYY-MM-DD}"), `\d{4}-\d{2}-\d{2}`,
	).Replace(regexp.QuoteMeta(pattern)) + "$")
	entries, err := os.ReadDir(filepath.Join(m.Root, filepath.FromSlash(hist)))
	if err != nil {
		return Result{}, err
	}
	byNumber := map[int][]string{}
	for _, e := range entries {
		if mm := fileRe.FindStringSubmatch(e.Name()); mm != nil && !e.IsDir() {
			n, _ := strconv.Atoi(mm[1])
			byNumber[n] = append(byNumber[n], e.Name())
			res.FilesLast = max(res.FilesLast, n)
		}
	}

	res.Date, res.DateVerdict = o.Today, "given"
	if res.Date == "" {
		c := clock.Today(root, 0)
		res.Date, res.DateVerdict, res.DateAdvice = c.Date, c.Verdict, c.Advice
	}
	if _, err := time.Parse(time.DateOnly, res.Date); err != nil {
		return Result{}, fmt.Errorf("today %q is not YYYY-MM-DD", res.Date)
	}

	res.Number = max(res.IndexLast, res.FilesLast) + 1
	res.Padded = fmt.Sprintf("%0*d", width, res.Number)
	res.File = strings.NewReplacer(tok, res.Padded, "{YYYY-MM-DD}", res.Date).Replace(pattern)
	res.Path = path.Join(hist, res.File)
	res.LastFiles = nonNil(byNumber[res.FilesLast])

	if eff, err := m.RolePath("effort"); err == nil {
		if _, err := os.Stat(filepath.Join(m.Root, filepath.FromSlash(eff))); err == nil {
			res.Effort = eff
		}
	} else if !errors.Is(err, manifest.ErrDisabled) {
		return Result{}, err
	}

	switch {
	case res.IndexLast > res.FilesLast:
		res.Advice = fmt.Sprintf("the index ends at %d and the folder at %d: a row was appended without its record "+
			"(or the record went elsewhere); check_index before writing, the number takes the higher", res.IndexLast, res.FilesLast)
	case res.IndexLast < res.FilesLast:
		res.Advice = fmt.Sprintf("the folder ends at %d and the index at %d: a record was written without its row; "+
			"check_index before writing, the number takes the higher", res.FilesLast, res.IndexLast)
	default:
		res.OK = true
		res.Advice = fmt.Sprintf("write %s, then append its row to %s", res.Path, res.Index)
		if res.Effort != "" {
			res.Advice += " and to " + res.Effort
		}
	}
	return res, nil
}

// lastNumber returns the highest number in the first column of the table
// that closes the index, or 0 if it has no rows yet.
func lastNumber(p string) (int, error) {
	lines, start, last, err := rows.ReadClosingTable(p)
	if err != nil {
		return 0, err
	}
	best := 0
	for i := start + 2; i <= last; i++ {
		cell := strings.TrimSpace(rows.SplitCells(lines[i])[0])
		n, err := strconv.Atoi(cell)
		if err != nil {
			return 0, fmt.Errorf("%s line %d: %q is not a session number", p, i+1, cell)
		}
		best = max(best, n)
	}
	return best, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
