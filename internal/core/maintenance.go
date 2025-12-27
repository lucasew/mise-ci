package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lucasew/mise-ci/internal/repository"
	"github.com/lucasew/mise-ci/internal/forge"
)

// FixMissingRepoURLs scans for runs without repo_url and attempts to populate it from git_link
func (c *Core) FixMissingRepoURLs(ctx context.Context, limit int) (int, error) {
	runs, err := c.repo.GetRunsWithoutRepoURL(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("failed to get runs: %w", err)
	}

	count := 0
	for _, run := range runs {
		if run.GitLink == "" {
			continue
		}

		// Expected git_link formats:
		// https://github.com/owner/repo/commit/sha
		// https://github.com/owner/repo/pull/123
		// We want to extract: https://github.com/owner/repo

		parts := strings.Split(run.GitLink, "/")
		if len(parts) < 5 {
			continue
		}

		// Reconstruct base repo URL (scheme://host/owner/repo)
		// parts[0] = "https:"
		// parts[1] = ""
		// parts[2] = "github.com"
		// parts[3] = "owner"
		// parts[4] = "repo"
		baseRepoURL := strings.Join(parts[:5], "/")

		// Check if this URL exists in repos table
		exists, err := c.repo.CheckRepoExists(ctx, baseRepoURL)
		if err != nil {
			c.logger.Error("failed to check repo existence", "url", baseRepoURL, "error", err)
			continue
		}

		targetURL := ""
		if exists {
			targetURL = baseRepoURL
		} else {
			// Try with .git suffix
			urlWithGit := baseRepoURL + ".git"
			existsWithGit, err := c.repo.CheckRepoExists(ctx, urlWithGit)
			if err != nil {
				c.logger.Error("failed to check repo existence", "url", urlWithGit, "error", err)
				continue
			}
			if existsWithGit {
				targetURL = urlWithGit
			}
		}

		if targetURL != "" {
			if err := c.repo.UpdateRunRepoURL(ctx, run.ID, targetURL); err != nil {
				c.logger.Error("failed to update run repo url", "run_id", run.ID, "url", targetURL, "error", err)
			} else {
				count++
			}
		} else {
			c.logger.Debug("repo not found for run", "run_id", run.ID, "git_link", run.GitLink, "derived_url", baseRepoURL)
		}
	}

	return count, nil
}

// extractSHA attempts to extract the SHA from the GitLink or RunID
func extractSHA(run *repository.RunMetadata) string {
    // Try GitLink first
    // https://github.com/owner/repo/commit/sha
    if strings.Contains(run.GitLink, "/commit/") {
        parts := strings.Split(run.GitLink, "/commit/")
        if len(parts) == 2 {
            return parts[1]
        }
    }

    // Try RunID: {short_sha}-{timestamp}
    if strings.Contains(run.ID, "-") {
        parts := strings.Split(run.ID, "-")
        // Short SHA is usually the first part
        return parts[0]
    }

    return ""
}

// CleanupStuckRuns marks runs as failed if they are stuck in scheduled/running state for too long
func (c *Core) CleanupStuckRuns(ctx context.Context, olderThan time.Time, limit int) (int, error) {
	runs, err := c.repo.GetStuckRuns(ctx, olderThan, limit)
	if err != nil {
		return 0, fmt.Errorf("failed to get stuck runs: %w", err)
	}

	count := 0
	for _, run := range runs {
		if !c.updateForgeStatus(ctx, run) {
			continue
		}

		// Update DB status
		// Using StatusError as per prompt "marca como failed" (StatusError or StatusFailure?)
		// "Internal error, please retry" suggests Error.
		if err := c.repo.UpdateRunStatus(ctx, run.ID, string(StatusError), nil); err != nil {
			c.logger.Error("failed to update run status in db", "run_id", run.ID, "error", err)
			continue
		}

		// Add a system log message
		logEntry := repository.LogEntry{
			Timestamp: time.Now(),
			Stream:    "system",
			Data:      "Run marked as failed by maintenance cleanup (stuck in scheduled/running)",
		}
		if err := c.repo.AppendLog(ctx, run.ID, logEntry); err != nil {
			c.logger.Warn("failed to append system log", "run_id", run.ID, "error", err)
		}

		count++
	}

	return count, nil
}

func (c *Core) updateForgeStatus(ctx context.Context, run *repository.RunMetadata) bool {
	if c.forge == nil {
		return true
	}

	sha := extractSHA(run)
	if sha == "" || run.RepoURL == "" {
		c.logger.Warn("skipping forge update due to missing info", "run_id", run.ID, "sha", sha, "repo_url", run.RepoURL)
		// If we can't identify the forge target, we might still want to clean up local DB?
		// Prompt says: "só commite quando tiver concluido os passos externos, se der erro o rollback é automágico"
		// This implies if external step *exists and fails*, rollback.
		// If external step is impossible (missing data), maybe we should still cleanup?
		// But safest is to skip cleanup if we can't update forge, to prompt manual intervention.
		// However, without SHA, manual intervention is also hard.
		// Let's assume we proceed if we simply CANNOT update forge (missing data), but fail if we TRY and fail.
		return true // Treat as success/not-needed
	}

	// We need to parse RepoURL to get "owner/repo" string if it's a URL
	repoSlug := run.RepoURL
	if strings.HasPrefix(repoSlug, "http") {
		// Extract path: https://github.com/owner/repo -> owner/repo
		parts := strings.Split(repoSlug, "/")
		if len(parts) >= 2 {
			repoSlug = strings.Join(parts[len(parts)-2:], "/")
			repoSlug = strings.TrimSuffix(repoSlug, ".git")
		}
	}

	err := c.forge.UpdateStatus(ctx, repoSlug, sha, forge.Status{
		State:       forge.StateError,
		Context:     "mise-ci",
		Description: "Internal error, please retry",
		TargetURL:   "", // No logs link available/relevant? Or maybe link to run page?
	})

	if err != nil {
		c.logger.Error("failed to update forge status", "run_id", run.ID, "error", err)
		// If forge update fails, we do NOT update DB.
		return false
	}
	return true
}
