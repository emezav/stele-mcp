// Package clock measures today's date from the machine clock and checks,
// against the time zone of recent commits, whether that clock lives where
// the project does.
//
// It does not know about MCP: the transport lives in cmd/stele-mcp.
package clock

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultCommits is how many commits the probe reads when none is given.
const DefaultCommits = 20

// Verdicts of the time zone probe.
const (
	Agree        = "agree"
	Inconclusive = "inconclusive"
	Disagree     = "disagree"
	NoVCS        = "no_vcs"
)

// Commit is a probed commit whose offset differs from the local one.
type Commit struct {
	Hash        string `json:"hash"`
	Date        string `json:"date"`
	Offset      string `json:"offset"`
	LocalOffset string `json:"local_offset"`
}

// Result is today's date and what the probe says about trusting it.
type Result struct {
	Date       string   `json:"date"`
	Time       string   `json:"time"`
	Offset     string   `json:"offset"`
	Zone       string   `json:"zone"`
	Verdict    string   `json:"verdict"`
	Advice     string   `json:"advice"`
	Checked    int      `json:"checked"`
	Mismatches []Commit `json:"mismatches,omitempty"`
}

// Today reads the machine clock and probes the last n commits of the git
// repository at dir ("" means the working directory; n <= 0 means
// DefaultCommits).
func Today(dir string, n int) Result {
	return today(time.Now(), dir, n)
}

type commit struct {
	hash   string
	at     time.Time
	date   string // as git printed it, in the commit's own offset
	offset string
}

func today(now time.Time, dir string, n int) Result {
	if n <= 0 {
		n = DefaultCommits
	}
	zone, _ := now.Zone()
	res := Result{
		Date:   now.Format("2006-01-02"),
		Time:   now.Format("15:04:05"),
		Offset: now.Format("-0700"),
		Zone:   zone,
	}

	commits, err := gitLog(dir, n)
	if err != nil {
		res.Verdict = NoVCS
		res.Advice = fmt.Sprintf("no commits to probe the time zone (%v): ask the user for the date", err)
		return res
	}
	res.Checked = len(commits)

	allUTC := true
	for _, c := range commits {
		local := c.at.In(now.Location()).Format("-0700")
		if c.offset != local {
			res.Mismatches = append(res.Mismatches, Commit{
				Hash:        c.hash,
				Date:        c.date,
				Offset:      c.offset,
				LocalOffset: local,
			})
		}
		if c.offset != "+0000" {
			allUTC = false
		}
	}

	switch {
	case len(res.Mismatches) > 0:
		res.Verdict = Disagree
		res.Advice = fmt.Sprintf("%d of %d commits were stamped in another time zone: "+
			"this clock may not be where the project lives; ask the user for the date",
			len(res.Mismatches), len(commits))
	case allUTC:
		res.Verdict = Inconclusive
		res.Advice = "clock and commits agree, but on +0000: the probe cannot tell a UTC machine " +
			"from a project that lives in UTC; ask the user for the date"
	default:
		res.Verdict = Agree
		res.Advice = fmt.Sprintf("clock and probed commits (%d) agree on the time zone: "+
			"the date can be trusted", len(commits))
	}
	return res
}

// gitLog returns the author date of the last n commits, newest first.
func gitLog(dir string, n int) ([]commit, error) {
	args := []string{"log", "-n", strconv.Itoa(n), "--format=%h %at %ai"}
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(strings.SplitN(msg, "\n", 2)[0])
		}
		return nil, err
	}

	var commits []commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// "abc1234 1793549594 2026-11-01 11:13:14 -0500"
		f := strings.Fields(line)
		if len(f) != 5 {
			continue
		}
		sec, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		commits = append(commits, commit{hash: f[0], at: time.Unix(sec, 0), date: strings.Join(f[2:], " "), offset: f[4]})
	}
	if len(commits) == 0 {
		return nil, errors.New("git log returned no commits")
	}
	return commits, nil
}
