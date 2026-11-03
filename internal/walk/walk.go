// Package walk lists the files under a path for the tools that read many
// files: recursively, without .git, without reading .gitignore, filtered by
// base-name globs that must each match some file.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package walk

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Collect lists the files to read under root, sorted, applying the globs. A
// glob that matches no file is an error: a filter that filters nothing reads
// exactly like one that worked.
func Collect(root string, include, exclude []string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	var all []string
	if info.IsDir() {
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() {
				all = append(all, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		all = []string{root}
	}

	used := map[string]bool{}
	var files []string
	for _, p := range all {
		base := filepath.Base(p)
		if len(include) > 0 && !matchAny(include, base, used) {
			continue
		}
		if matchAny(exclude, base, used) {
			continue
		}
		files = append(files, p)
	}
	for _, g := range append(append([]string{}, include...), exclude...) {
		if _, err := filepath.Match(g, ""); err != nil {
			return nil, fmt.Errorf("bad glob %q: %v", g, err)
		}
		if !used[g] {
			return nil, fmt.Errorf("glob %q matches no file under %s", g, root)
		}
	}
	sort.Strings(files)
	return files, nil
}

// matchAny reports whether base matches one of globs, and marks every glob
// that matches as used, so a dead filter can be reported.
func matchAny(globs []string, base string, used map[string]bool) bool {
	hit := false
	for _, g := range globs {
		if ok, _ := filepath.Match(g, base); ok {
			used[g] = true
			hit = true
		}
	}
	return hit
}

// Relative turns p into a path relative to root with forward slashes. When
// root is the file itself the path is kept as given.
func Relative(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}
