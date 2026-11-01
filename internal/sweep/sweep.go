// Package sweep searches the Markdown under a path through markgrep's search
// package, which matches whole units (paragraph, list item, table, ...)
// instead of lines, so a phrase split by a line break is still found. It adds
// only what a caller over MCP needs: JSON-tagged types and a cap on how many
// matches come back, reported instead of silent.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package sweep

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/emezav/markgrep/search"
)

// DefaultLimit is how many matches come back when Options.Limit is not set.
const DefaultLimit = 100

// ErrEmptyCorpus is returned when no Markdown file is left to search.
var ErrEmptyCorpus = errors.New("empty corpus")

// Options mirrors search.Options, plus Limit.
type Options struct {
	Literal       bool
	CaseSensitive bool
	Invert        bool
	Kind          string
	Links         string
	Include       []string
	Exclude       []string
	Control       []string
	Limit         int // matches returned; <= 0 means DefaultLimit
}

// Match is one unit where a pattern hit.
type Match struct {
	Path     string   `json:"path"`
	Line     int      `json:"line"`
	Kind     string   `json:"kind"`
	Heading  string   `json:"heading"`
	Parent   string   `json:"parent,omitempty"`
	Text     string   `json:"text"`
	Patterns []string `json:"patterns"`
}

// Result is the corpus searched and what matched in it. Paths in Files and
// Matches are relative to Root, with forward slashes. Matched counts every
// match; Matches holds at most Limit of them, and Truncated says so.
type Result struct {
	Root      string   `json:"root"`
	Files     []string `json:"files"`
	Units     int      `json:"units"`
	Matched   int      `json:"matched"`
	Truncated bool     `json:"truncated"`
	Matches   []Match  `json:"matches"`
	Grep      []string `json:"grep"`
}

// Run searches the Markdown files under root (a file or a directory) for
// patterns. Every refusal from search.Run is returned as an error, never as an
// empty Result: a dead filter (search.ErrFilterMatchesNothing) and a failed
// control (search.ErrControlFailed) included. An empty corpus is refused too
// (ErrEmptyCorpus).
func Run(root string, patterns []string, o Options) (Result, error) {
	if root == "" {
		return Result{}, errors.New("path is required: the file or directory to search")
	}
	r, err := search.Run(root, patterns, search.Options{
		Literal:  o.Literal,
		CaseSens: o.CaseSensitive,
		Invert:   o.Invert,
		Kind:     o.Kind,
		Links:    o.Links,
		Include:  o.Include,
		Exclude:  o.Exclude,
		Control:  o.Control,
	})
	if err != nil {
		return Result{}, err
	}
	// search.Run accepts an empty corpus when each filter matched something
	// but together they left nothing, or when root holds no Markdown at all.
	// Zero matches over zero files reads exactly like a clean corpus.
	if len(r.Files) == 0 {
		return Result{}, fmt.Errorf("no Markdown file left to search under %s after the filters: %w", root, ErrEmptyCorpus)
	}

	limit := o.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	files := make([]string, len(r.Files))
	for i, f := range r.Files {
		files[i] = relative(root, f)
	}
	res := Result{
		Root:    root,
		Files:   files,
		Units:   r.Units,
		Matched: len(r.Matches),
		Matches: []Match{},
		Grep:    nonNil(r.Grep),
	}
	for i, m := range r.Matches {
		if i == limit {
			res.Truncated = true
			break
		}
		res.Matches = append(res.Matches, Match{
			Path:     relative(root, m.U.Path),
			Line:     m.U.Line,
			Kind:     m.U.Kind,
			Heading:  m.U.Heading,
			Parent:   m.U.Parent,
			Text:     m.U.Raw(),
			Patterns: nonNil(m.Patterns),
		})
	}
	return res, nil
}

// relative turns a path from search, which comes joined to root, into one
// relative to root with forward slashes. When root is the file itself the
// path is kept as given.
func relative(root, p string) string {
	rel, err := filepath.Rel(root, filepath.FromSlash(p))
	if err != nil || rel == "." {
		return p
	}
	return filepath.ToSlash(rel)
}

// nonNil keeps an empty list an empty JSON array instead of null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
