// Package budget measures the documents of a stele instance against the line
// budgets of its manifest (the Presupuestos section). It reads the budgets
// from the manifest instead of carrying its own, so a budget raised there is
// the one measured here.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package budget

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/emezav/stele-mcp/internal/manifest"
)

// Options controls where the manifest is.
type Options struct {
	// Config is the manifest path; empty means stele.config.md under root.
	Config string
}

// Budget is one measured row of the budgets table.
type Budget struct {
	Role    string `json:"role"`
	File    string `json:"file"`
	Lines   int    `json:"lines"`
	Max     int    `json:"max"`
	Over    int    `json:"over"`
	OK      bool   `json:"ok"`
	Missing bool   `json:"missing"`
	Line    int    `json:"line"`
}

// Skipped is a row of the budgets table that was not measured, and why.
type Skipped struct {
	Key    string `json:"key"`
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Result is what was measured. OK is true when every budget holds.
type Result struct {
	Config  string    `json:"config"`
	Section string    `json:"section"`
	OK      bool      `json:"ok"`
	Budgets []Budget  `json:"budgets"`
	Skipped []Skipped `json:"skipped"`
}

// Check reads the budgets section of the manifest of the instance at root
// and counts the lines of each role's document. A row whose key is not a
// document role, such as the kit's own budgets, or whose budget is the
// sentinel, is skipped and listed with its reason. A missing document is
// reported, not skipped. It fails if a budget is not a whole number, or if
// no row is measured, since zero budgets over zero documents reads like a
// clean instance.
func Check(root string, opts Options) (Result, error) {
	if root == "" {
		root = "."
	}
	cfg := opts.Config
	if cfg == "" {
		cfg = filepath.Join(root, manifest.DefaultName)
	}
	m, err := manifest.Read(cfg)
	if err != nil {
		return Result{}, err
	}
	sec := m.Section(manifest.Budgets)
	res := Result{Config: cfg, Section: sec.Title, OK: true}
	for _, r := range sec.Rows {
		if !manifest.IsRole(r.Key) {
			res.Skipped = append(res.Skipped, Skipped{Key: r.Key, Line: r.Line, Reason: "not a document role"})
			continue
		}
		if manifest.IsSentinel(r.Value) {
			res.Skipped = append(res.Skipped, Skipped{Key: r.Key, Line: r.Line, Reason: "no budget (sentinel)"})
			continue
		}
		limit, ok := wholeNumber(r.Value)
		if !ok {
			return Result{}, fmt.Errorf("%s line %d: budget of %s is %q, not a whole number", cfg, r.Line, r.Key, r.Value)
		}
		rel, err := m.RolePath(r.Key)
		if errors.Is(err, manifest.ErrDisabled) {
			res.Skipped = append(res.Skipped, Skipped{Key: r.Key, Line: r.Line, Reason: "role disabled in the names table"})
			continue
		}
		if err != nil {
			return Result{}, err
		}
		b := Budget{Role: r.Key, File: rel, Max: limit, Line: r.Line}
		data, err := os.ReadFile(filepath.Join(m.Root, filepath.FromSlash(rel)))
		switch {
		case errors.Is(err, os.ErrNotExist):
			b.Missing = true
		case err != nil:
			return Result{}, err
		default:
			b.Lines = countLines(data)
			b.Over = max(0, b.Lines-b.Max)
			b.OK = b.Over == 0
		}
		res.OK = res.OK && b.OK
		res.Budgets = append(res.Budgets, b)
	}
	if len(res.Budgets) == 0 {
		return Result{}, fmt.Errorf("%s: no row of section %q measures a document role (%d skipped); is it the budgets section?",
			cfg, sec.Title, len(res.Skipped))
	}
	if res.Skipped == nil {
		res.Skipped = []Skipped{}
	}
	return res, nil
}

// countLines counts lines as an editor shows them: a last line without a
// newline still counts, unlike wc -l.
func countLines(data []byte) int {
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func wholeNumber(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
