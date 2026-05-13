package rules

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/state"
)

// R11CommitReview detects substantial new commits in any active project and
// fires one Trigger per commit so the brain can do a mentor-mode review.
// "Substantial" = >5 files changed OR >200 LOC delta.
//
// Per project, we remember the last reviewed commit SHA so we don't re-review
// the same one. The first time we see a project, we set the baseline to HEAD
// (no historical backfill — only commits made AFTER pace started watching).
type R11CommitReview struct {
	mu              sync.Mutex
	lastReviewedSHA map[string]string // project_path -> sha
}

func (*R11CommitReview) Name() string { return "R11.commit_review" }

func (r *R11CommitReview) Evaluate(ctx context.Context, s *state.State, now time.Time) ([]Trigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastReviewedSHA == nil {
		r.lastReviewedSHA = map[string]string{}
	}

	// Fetch active project paths from state.
	rows, err := s.DB().Query(`SELECT project_path FROM projects WHERE paused_until IS NULL OR paused_until > datetime('now')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var triggers []Trigger
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		t := r.evaluateProject(ctx, p, now)
		if t != nil {
			triggers = append(triggers, *t)
		}
	}
	return triggers, nil
}

func (r *R11CommitReview) evaluateProject(ctx context.Context, project string, now time.Time) *Trigger {
	headSHA := gitHead(ctx, project)
	if headSHA == "" {
		return nil
	}
	last, seen := r.lastReviewedSHA[project]
	if !seen {
		// First sighting — baseline at HEAD, don't review historical commits.
		r.lastReviewedSHA[project] = headSHA
		return nil
	}
	if last == headSHA {
		return nil
	}
	// New commits exist. Check if the most recent is substantial.
	files, loc := gitDiffStats(ctx, project, last, headSHA)
	r.lastReviewedSHA[project] = headSHA
	if files < 5 && loc < 200 {
		return nil
	}
	subject := gitSubject(ctx, project, headSHA)
	return &Trigger{
		RuleName:    "R11.commit_review",
		ProjectPath: project,
		Reason: "substantial commit detected (" + strconv.Itoa(files) + " files, " +
			strconv.Itoa(loc) + " LOC): " + subject + " [" + shortSHA(headSHA) + "]",
		Now: now,
	}
}

// ─── git helpers (best-effort, fail silent) ─────────────────────────────

func gitHead(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitSubject(ctx context.Context, dir, sha string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "log", "-1", "--format=%s", sha).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitDiffStats returns (files_changed, additions+deletions) between since..head.
// Returns (0,0) on error.
func gitDiffStats(ctx context.Context, dir, since, head string) (int, int) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "diff", "--shortstat", since+".."+head).Output()
	if err != nil {
		return 0, 0
	}
	// Format: " 3 files changed, 41 insertions(+), 12 deletions(-)"
	s := strings.TrimSpace(string(out))
	files, ins, del := 0, 0, 0
	parts := strings.Split(s, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if i := strings.Index(p, " "); i > 0 {
			n, _ := strconv.Atoi(p[:i])
			rest := p[i+1:]
			switch {
			case strings.HasPrefix(rest, "file"):
				files = n
			case strings.HasPrefix(rest, "insertion"):
				ins = n
			case strings.HasPrefix(rest, "deletion"):
				del = n
			}
		}
	}
	return files, ins + del
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
