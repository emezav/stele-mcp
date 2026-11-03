// Package manifest reads the manifest of a stele instance (stele.config.md):
// its sections, by position as its parsing contract says, the key and value
// of each table row, and the path of each role.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/emezav/stele-mcp/internal/rows"
)

// DefaultName is the manifest file name at the project root.
const DefaultName = "stele.config.md"

// Section positions fixed by the manifest's parsing contract: the ## headers
// are canonical and come in this order, and are referenced by position, not
// by their text, which follows the project's language.
const (
	Meta = iota
	Paths
	Names
	Features
	Budgets
	Wording
)

var sectionNames = []string{"Meta", "Rutas", "Nombres", "Features", "Presupuestos", "Wording"}

// Row is one row of a section table: column 1 is the key, column 2 the value.
type Row struct {
	Key   string
	Value string
	Line  int
}

// Section is a ## section and the rows of its first table.
type Section struct {
	Title string
	Line  int
	Rows  []Row
}

// Manifest is a parsed manifest.
type Manifest struct {
	// Path is the manifest file; Root is its folder, the project root.
	Path     string
	Root     string
	Sections []Section
}

// emDash is the sentinel the manifest writes for an empty value.
var emDash = string(rune(0x2014))

// IsSentinel reports whether v is the manifest's empty value: an em dash, or
// its ASCII forms --- and -, which the manifest accepts as the same sentinel.
func IsSentinel(v string) bool {
	v = strings.TrimSpace(v)
	return v == emDash || v == "---" || v == "-"
}

// Read parses the manifest at p. It fails if the file does not exist or has
// fewer sections than the contract names.
func Read(p string) (*Manifest, error) {
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("manifest does not exist: %s", p)
	}
	if err != nil {
		return nil, err
	}
	m := &Manifest{Path: p, Root: filepath.Dir(p)}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	fence := ""
	var cur *Section
	inTable, tableDone := false, false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if fence != "" {
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = t[:3]
			inTable = false
			continue
		}
		if strings.HasPrefix(l, "## ") {
			m.Sections = append(m.Sections, Section{Title: strings.TrimSpace(l[3:]), Line: i + 1})
			cur = &m.Sections[len(m.Sections)-1]
			inTable, tableDone = false, false
			continue
		}
		if cur == nil || tableDone {
			continue
		}
		if !rows.IsTableLine(l) {
			if inTable {
				inTable, tableDone = false, true
			}
			continue
		}
		if !inTable {
			// The header line; the separator is skipped below.
			inTable = true
			continue
		}
		cells := rows.SplitCells(l)
		if len(cells) < 2 || isSeparator(cells) {
			continue
		}
		cur.Rows = append(cur.Rows, Row{Key: clean(cells[0]), Value: clean(cells[1]), Line: i + 1})
	}
	if len(m.Sections) < len(sectionNames) {
		return nil, fmt.Errorf("%s has %d ## sections, the contract names %d (%s)",
			p, len(m.Sections), len(sectionNames), strings.Join(sectionNames, ", "))
	}
	return m, nil
}

// Section returns the section at position pos (Meta, Paths...).
func (m *Manifest) Section(pos int) Section {
	return m.Sections[pos]
}

// Value returns the value of key in the section at pos, and whether the row
// exists. A missing row means the default, which is not the same as the
// sentinel: the caller decides what each one means.
func (m *Manifest) Value(pos int, key string) (string, bool) {
	for _, r := range m.Sections[pos].Rows {
		if strings.EqualFold(r.Key, key) {
			return r.Value, true
		}
	}
	return "", false
}

// Role locations, from the kit's role tables (core/roles.md and the producto
// module): under base, or under one of its containers.
var locations = map[string]string{
	"entry": "", "gotchas": "", "charter": "", "manual": "", "protocol": "",
	"specs": "", "architecture": "",
	"state": "history_dir", "handover": "history_dir", "index": "history_dir",
	"audit": "history_dir", "effort": "history_dir",
	"correspondence": "correspondence_dir",
}

// defaults are the role and container names the kit uses when the manifest
// has no row for them.
var defaults = map[string]string{
	"entry": "guia-agente.md", "gotchas": "memory.md", "charter": "design.md",
	"manual": "manual.md", "protocol": "protocol.md", "specs": "requirements.md",
	"architecture": "architecture.md", "state": "latest.md", "handover": "handover.md",
	"index": "index.md", "audit": "audit.md", "effort": "esfuerzo.md",
	"correspondence": "cartas.md", "history_dir": "history/",
	"correspondence_dir": "correspondencia/",
}

// IsRole reports whether role is a document role this package can locate.
func IsRole(role string) bool {
	_, ok := locations[role]
	return ok
}

// ErrDisabled is returned by RolePath for a role whose name is the sentinel.
var ErrDisabled = errors.New("role is disabled in the manifest")

// Base returns the base path from the Paths section, "." if it is the root.
func (m *Manifest) Base() string {
	b, ok := m.Value(Paths, "base")
	if !ok || b == "" {
		return "."
	}
	return strings.TrimSuffix(b, "/")
}

// RolePath returns the path of a document role relative to the project root,
// with forward slashes: base, then its container if it has one, then the
// name the manifest gives it (or the kit default if there is no row).
func (m *Manifest) RolePath(role string) (string, error) {
	loc, ok := locations[role]
	if !ok {
		return "", fmt.Errorf("unknown role %q", role)
	}
	name, err := m.name(role)
	if err != nil {
		return "", err
	}
	dir := ""
	if loc != "" {
		if dir, err = m.name(loc); err != nil {
			return "", fmt.Errorf("container %s: %w", loc, err)
		}
	}
	return path.Clean(path.Join(m.Base(), dir, name)), nil
}

func (m *Manifest) name(role string) (string, error) {
	v, ok := m.Value(Names, role)
	if !ok {
		return defaults[role], nil
	}
	if IsSentinel(v) {
		return "", ErrDisabled
	}
	return v, nil
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(strings.TrimSpace(c), ":-") != "" {
			return false
		}
	}
	return true
}

// clean trims a cell and the backticks around a code span.
func clean(c string) string {
	c = strings.TrimSpace(c)
	if len(c) >= 2 && strings.HasPrefix(c, "`") && strings.HasSuffix(c, "`") {
		c = c[1 : len(c)-1]
	}
	return strings.TrimSpace(c)
}
