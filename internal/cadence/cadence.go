// Package cadence answers the two reminders of the close ritual of a stele
// instance: whether an audit is due (sessions since the range of the last
// audit row against audit_every_n_sessions) and whether the kit is due a
// look at its origin (days since kit_revisado against update_every_n_days).
// Neither one triggers anything; they are what the close writes in the state
// document, and this reads the three documents that hold the numbers.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package cadence

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emezav/stele-mcp/internal/manifest"
	"github.com/emezav/stele-mcp/internal/rows"
)

// Statuses.
const (
	OK         = "ok"
	Overdue    = "overdue"
	Off        = "off"
	NoReminder = "no_reminder"
)

// Options controls where the manifest is and which day and session count as
// now.
type Options struct {
	// Config is the manifest path; empty means stele.config.md under root.
	Config string
	// Today is the date YYYY-MM-DD; empty means the machine's local date.
	Today string
	// Session is the current session number; 0 means the highest number in
	// the session index, which at close time already has this session's row.
	Session int
}

// Audit is the audit reminder.
type Audit struct {
	Status      string `json:"status"`
	Every       int    `json:"every"`
	Log         string `json:"log"`
	Audits      int    `json:"audits"`
	LastCovered int    `json:"last_covered"`
	Session     int    `json:"session"`
	Since       int    `json:"since"`
	Advice      string `json:"advice"`
}

// Kit is the kit review reminder.
type Kit struct {
	Status   string `json:"status"`
	Origin   string `json:"origin"`
	Reviewed string `json:"reviewed"`
	Every    int    `json:"every"`
	Days     int    `json:"days"`
	Advice   string `json:"advice"`
}

// Result holds both reminders. OK is true when neither is overdue.
type Result struct {
	Config string `json:"config"`
	Today  string `json:"today"`
	OK     bool   `json:"ok"`
	Audit  Audit  `json:"audit"`
	Kit    Kit    `json:"kit"`
}

var numberRe = regexp.MustCompile(`\d+`)

// Check reads the manifest of the instance at root, the session index and
// the audit log, and computes both reminders. It fails if a value it needs
// is malformed: a threshold that is not a whole number, a date that is not
// YYYY-MM-DD, or an index with no session number.
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
	today := o.Today
	if today == "" {
		today = time.Now().Format(time.DateOnly)
	}
	now, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return Result{}, fmt.Errorf("today %q is not YYYY-MM-DD", today)
	}
	res := Result{Config: cfg, Today: today}
	if res.Audit, err = audit(m, o.Session); err != nil {
		return Result{}, err
	}
	if res.Kit, err = kit(m, now); err != nil {
		return Result{}, err
	}
	res.OK = res.Audit.Status != Overdue && res.Kit.Status != Overdue
	return res, nil
}

func audit(m *manifest.Manifest, session int) (Audit, error) {
	a := Audit{}
	if v, ok := m.Value(manifest.Features, "audit_log"); ok && strings.EqualFold(v, "off") {
		a.Status, a.Advice = Off, "audit_log is off: there is no audit series to count from"
		return a, nil
	}
	v, ok := m.Value(manifest.Features, "audit_every_n_sessions")
	if !ok {
		v = "10" // the kit default
	}
	if manifest.IsSentinel(v) {
		a.Status, a.Advice = NoReminder, "audit_every_n_sessions is the sentinel: no reminder"
		return a, nil
	}
	every, err := strconv.Atoi(v)
	if err != nil || every <= 0 {
		return Audit{}, fmt.Errorf("audit_every_n_sessions is %q, not a whole number", v)
	}
	a.Every = every

	if session <= 0 {
		idx, err := m.RolePath("index")
		if err != nil {
			return Audit{}, fmt.Errorf("index: %w", err)
		}
		if session, err = lastSession(filepath.Join(m.Root, filepath.FromSlash(idx))); err != nil {
			return Audit{}, err
		}
	}
	a.Session = session

	log, err := m.RolePath("audit")
	if err != nil {
		return Audit{}, fmt.Errorf("audit: %w", err)
	}
	a.Log = log
	p := filepath.Join(m.Root, filepath.FromSlash(log))
	if _, err := os.Stat(p); err == nil {
		// The first audit creates the log: no log means never audited.
		lines, start, last, err := rows.ReadClosingTable(p)
		if err != nil {
			return Audit{}, err
		}
		a.Audits = last - start - 1
		if a.Audits > 0 {
			cells := rows.SplitCells(lines[last])
			if len(cells) < 3 {
				return Audit{}, fmt.Errorf("%s line %d: no Sesiones column (the third)", log, last+1)
			}
			nums := numberRe.FindAllString(cells[2], -1)
			if len(nums) == 0 {
				return Audit{}, fmt.Errorf("%s line %d: the session range %q has no number", log, last+1, strings.TrimSpace(cells[2]))
			}
			a.LastCovered, _ = strconv.Atoi(nums[len(nums)-1])
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Audit{}, err
	}
	a.Since = a.Session - a.LastCovered
	switch {
	case a.Since > a.Every && a.Audits == 0:
		a.Status = Overdue
		a.Advice = fmt.Sprintf("never audited, and %d sessions exceed the cadence of %d: an audit is due", a.Since, a.Every)
	case a.Since > a.Every:
		a.Status = Overdue
		a.Advice = fmt.Sprintf("%d sessions since the last audit covered session %d, more than %d: an audit is due", a.Since, a.LastCovered, a.Every)
	default:
		a.Status = OK
		a.Advice = fmt.Sprintf("%d sessions since session %d; due when closing session %d", a.Since, a.LastCovered, a.LastCovered+a.Every+1)
	}
	return a, nil
}

func kit(m *manifest.Manifest, now time.Time) (Kit, error) {
	k := Kit{}
	origin, _ := m.Value(manifest.Meta, "kit_origen")
	reviewed, _ := m.Value(manifest.Meta, "kit_revisado")
	v, ok := m.Value(manifest.Features, "update_every_n_days")
	if !ok {
		v = "30" // the kit default
	}
	k.Origin, k.Reviewed = origin, reviewed
	switch {
	case origin == "" || manifest.IsSentinel(origin):
		k.Status, k.Advice = NoReminder, "no kit_origen: there is no origin to look at"
		return k, nil
	case reviewed == "" || manifest.IsSentinel(reviewed):
		k.Status, k.Advice = NoReminder, "no kit_revisado: no reminder"
		return k, nil
	case manifest.IsSentinel(v):
		k.Status, k.Advice = NoReminder, "update_every_n_days is the sentinel: no reminder"
		return k, nil
	}
	every, err := strconv.Atoi(v)
	if err != nil || every <= 0 {
		return Kit{}, fmt.Errorf("update_every_n_days is %q, not a whole number", v)
	}
	k.Every = every
	at, err := time.Parse(time.DateOnly, reviewed)
	if err != nil {
		return Kit{}, fmt.Errorf("kit_revisado is %q, not YYYY-MM-DD", reviewed)
	}
	k.Days = int(now.Sub(at).Hours() / 24)
	if k.Days > every {
		k.Status = Overdue
		k.Advice = fmt.Sprintf("%d days since the kit origin was last looked at (%s), more than %d: time to look (ACTUALIZAR)", k.Days, reviewed, every)
	} else {
		k.Status = OK
		k.Advice = fmt.Sprintf("%d days since %s; due after %s", k.Days, reviewed, at.AddDate(0, 0, every).Format(time.DateOnly))
	}
	return k, nil
}

// lastSession returns the highest number in the first column of the table
// that closes the session index.
func lastSession(path string) (int, error) {
	lines, start, last, err := rows.ReadClosingTable(path)
	if err != nil {
		return 0, err
	}
	best := 0
	for i := start + 2; i <= last; i++ {
		cells := rows.SplitCells(lines[i])
		if n, err := strconv.Atoi(strings.TrimSpace(cells[0])); err == nil && n > best {
			best = n
		}
	}
	if best == 0 {
		return 0, fmt.Errorf("%s: no session number in the first column", path)
	}
	return best, nil
}
