// Package handover checks whether the handover of a stele instance is still
// current: it reads the state and the seal (the HEAD commit written when the
// checkpoint was opened or closed) and compares the seal with today's HEAD.
// A seal that differs means work happened after the handover was written,
// and the tree, not the handover, says where things are.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package handover

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/emezav/stele-mcp/internal/manifest"
)

// Verdicts.
const (
	Current = "current"
	Stale   = "stale"
	Unknown = "unknown"
	NoSeal  = "no_seal"
	NoVCS   = "no_vcs"
)

// Idle is the state of a handover with no work in progress.
const Idle = "SIN_TRABAJO_ACTIVO"

// Section titles, in the kit's language and in English, compared without case.
var (
	stateTitles = []string{"estado", "state", "status"}
	sealTitles  = []string{"sello", "seal"}
)

var hashRe = regexp.MustCompile(`\b[0-9a-fA-F]{7,40}\b`)

// Options controls where the handover and the repository are.
type Options struct {
	// Config is the manifest path; empty means stele.config.md under root.
	Config string
	// Path is the handover file; empty means the handover role of the
	// manifest. Relative paths resolve against the working directory.
	Path string
	// Dir is the git repository; empty means root.
	Dir string
}

// Result is what the check found. OK is true when the verdict is current.
type Result struct {
	Handover string `json:"handover"`
	State    string `json:"state"`
	Active   bool   `json:"active"`
	Seal     string `json:"seal"`
	SealLine int    `json:"seal_line"`
	Head     string `json:"head"`
	Resolved string `json:"resolved"`
	Verdict  string `json:"verdict"`
	Ancestor bool   `json:"ancestor"`
	Behind   int    `json:"behind"`
	Dirty    int    `json:"dirty"`
	OK       bool   `json:"ok"`
	Advice   string `json:"advice"`
}

// Check reads the handover of the instance at root and compares its seal with
// HEAD. It fails if the handover does not exist or has no state; a missing
// seal or repository is a verdict, not an error.
func Check(root string, o Options) (Result, error) {
	if root == "" {
		root = "."
	}
	p := o.Path
	if p == "" {
		cfg := o.Config
		if cfg == "" {
			cfg = filepath.Join(root, manifest.DefaultName)
		}
		m, err := manifest.Read(cfg)
		if err != nil {
			return Result{}, fmt.Errorf("%w; or give the handover path", err)
		}
		rel, err := m.RolePath("handover")
		if err != nil {
			return Result{}, fmt.Errorf("handover: %w", err)
		}
		p = filepath.Join(m.Root, filepath.FromSlash(rel))
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("handover does not exist: %s", p)
	}
	if err != nil {
		return Result{}, err
	}
	res := Result{Handover: filepath.ToSlash(p)}

	state, _, ok := section(string(data), stateTitles)
	if !ok {
		return Result{}, fmt.Errorf("%s has no ## Estado (or State, Status) section", p)
	}
	for _, l := range state {
		if f := strings.Fields(strings.Trim(strings.TrimSpace(l), "*`_")); len(f) > 0 {
			res.State = strings.Trim(f[0], "*`_")
			break
		}
	}
	if res.State == "" {
		return Result{}, fmt.Errorf("the state section of %s is empty", p)
	}
	res.Active = res.State != Idle

	seal, first, _ := section(string(data), sealTitles)
	for i, l := range seal {
		if h := hashRe.FindString(l); h != "" {
			res.Seal, res.SealLine = strings.ToLower(h), first+i
			break
		}
	}

	dir := o.Dir
	if dir == "" {
		dir = root
	}
	head, err := git(dir, "rev-parse", "HEAD")
	switch {
	case err != nil:
		res.Verdict = NoVCS
		res.Advice = "no git repository at " + dir + " (" + err.Error() + "): compare the handover with what is on disk"
		return res, nil
	case res.Seal == "":
		res.Head = head
		res.Verdict = NoSeal
		res.Advice = "the handover has no seal: nothing says when it was written, so check it against the tree"
		return res, nil
	}
	res.Head = head
	if out, err := git(dir, "status", "--porcelain", "--untracked-files=no"); err == nil && out != "" {
		res.Dirty = len(strings.Split(out, "\n"))
	}
	resolved, err := git(dir, "rev-parse", "--verify", "--quiet", res.Seal+"^{commit}")
	if err != nil || resolved == "" {
		res.Verdict = Unknown
		res.Advice = "the seal " + res.Seal + " is not a commit of this repository (or is ambiguous): the handover cannot be dated"
		return res, nil
	}
	res.Resolved = resolved
	if resolved == head {
		res.Verdict, res.OK = Current, true
		res.Advice = "the seal is HEAD: the handover describes the current tree"
		if res.Dirty > 0 {
			res.Advice += fmt.Sprintf(", plus %d uncommitted change(s) to versioned files", res.Dirty)
		}
		return res, nil
	}
	res.Verdict = Stale
	if _, err := git(dir, "merge-base", "--is-ancestor", resolved, head); err == nil {
		res.Ancestor = true
		if n, err := git(dir, "rev-list", "--count", resolved+"..HEAD"); err == nil {
			res.Behind, _ = strconv.Atoi(n)
		}
		res.Advice = fmt.Sprintf("HEAD is %d commit(s) after the seal: work happened after the handover was written, so the tree rules", res.Behind)
	} else {
		res.Advice = "the seal is not an ancestor of HEAD (history rewritten, or another branch): the tree rules"
	}
	return res, nil
}

// section returns the lines of the first ## section whose title is one of
// titles, the line number of its first line, and whether it was found.
// Fenced code is not read.
func section(text string, titles []string) ([]string, int, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	fence := false
	var out []string
	in, found, first := false, false, 0
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
		}
		if !fence && strings.HasPrefix(l, "## ") {
			if in {
				break
			}
			title := strings.ToLower(strings.TrimSpace(l[3:]))
			for _, want := range titles {
				if title == want {
					in, found, first = true, true, i+2
				}
			}
			continue
		}
		if in {
			out = append(out, l)
		}
	}
	return out, first, found
}

// git runs git -C dir args and returns its trimmed output, or the first line
// of its error output as the error.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(strings.SplitN(msg, "\n", 2)[0])
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
