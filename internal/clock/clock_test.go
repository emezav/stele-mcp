package clock

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // America/New_York on machines without a zone database; test binary only
)

var bogota = time.FixedZone("-05", -5*3600)

// repo creates a git repository with one empty commit per author date
// (RFC 3339), oldest first.
func repo(t *testing.T, dates ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	git(t, dir, nil, "init", "-q")
	for _, d := range dates {
		env := []string{"GIT_AUTHOR_DATE=" + d, "GIT_COMMITTER_DATE=" + d}
		git(t, dir, env, "-c", "user.name=t", "-c", "user.email=t@example.com",
			"-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "--no-verify", "-m", d)
	}
	return dir
}

func git(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestTodayUsesLocalDateNotUTC(t *testing.T) {
	// 23:30 in Bogota is already the next day in UTC.
	now := time.Date(2026, 11, 1, 23, 30, 0, 0, bogota)
	res := today(now, repo(t, "2026-10-30T10:00:00-05:00"), 0)
	if res.Date != "2026-11-01" || res.Time != "23:30:00" || res.Offset != "-0500" {
		t.Fatalf("result = %+v", res)
	}
}

func TestTodayAgree(t *testing.T) {
	now := time.Date(2026, 11, 1, 11, 0, 0, 0, bogota)
	res := today(now, repo(t, "2026-10-30T10:00:00-05:00", "2026-10-31T18:00:00-05:00"), 0)
	if res.Verdict != Agree || res.Checked != 2 || len(res.Mismatches) != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestTodayDisagreeOnRemoteMachine(t *testing.T) {
	// The server runs in UTC; the project commits from Bogota.
	now := time.Date(2026, 11, 2, 4, 30, 0, 0, time.UTC)
	res := today(now, repo(t, "2026-10-30T10:00:00-05:00", "2026-10-31T18:00:00+00:00"), 0)
	if res.Verdict != Disagree || len(res.Mismatches) != 1 {
		t.Fatalf("result = %+v", res)
	}
	m := res.Mismatches[0]
	if m.Offset != "-0500" || m.LocalOffset != "+0000" || m.Date != "2026-10-30 10:00:00 -0500" {
		t.Fatalf("mismatch = %+v", m)
	}
	if !strings.Contains(res.Advice, "ask the user") {
		t.Fatalf("advice = %q", res.Advice)
	}
}

func TestTodayInconclusiveOnUTC(t *testing.T) {
	now := time.Date(2026, 11, 1, 11, 0, 0, 0, time.UTC)
	res := today(now, repo(t, "2026-10-30T10:00:00Z"), 0)
	if res.Verdict != Inconclusive || len(res.Mismatches) != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestTodayComparesOffsetAtCommitInstant(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// November is EST (-0500); a July commit was stamped in EDT (-0400).
	now := time.Date(2026, 11, 1, 11, 0, 0, 0, ny)
	res := today(now, repo(t, "2026-07-15T10:00:00-04:00", "2026-10-30T10:00:00-04:00"), 0)
	if res.Verdict != Agree || res.Offset != "-0500" {
		t.Fatalf("result = %+v", res)
	}
}

func TestTodayLimitsCommits(t *testing.T) {
	now := time.Date(2026, 11, 1, 11, 0, 0, 0, bogota)
	// The oldest commit disagrees but falls outside the last 2.
	dir := repo(t, "2026-10-28T10:00:00+00:00", "2026-10-30T10:00:00-05:00", "2026-10-31T10:00:00-05:00")
	if res := today(now, dir, 2); res.Verdict != Agree || res.Checked != 2 {
		t.Fatalf("result = %+v", res)
	}
	if res := today(now, dir, 3); res.Verdict != Disagree {
		t.Fatalf("result = %+v", res)
	}
}

func TestTodayNoVCS(t *testing.T) {
	now := time.Date(2026, 11, 1, 11, 0, 0, 0, bogota)
	for name, dir := range map[string]string{
		"no commits":  repo(t),
		"no such dir": t.TempDir() + "/missing",
	} {
		res := today(now, dir, 0)
		if res.Verdict != NoVCS || res.Date != "2026-11-01" || !strings.Contains(res.Advice, "ask the user") {
			t.Errorf("%s: result = %+v", name, res)
		}
	}
}
