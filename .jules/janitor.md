# Janitor's Journal

This journal is for CRITICAL, reusable learnings only. See prompt for what to record.
## 2026-01-10 - Add Logging to Markdown Rendering Fallback

**Issue:** The `markdown.Render` function had a fallback mechanism to return plain text if the markdown conversion failed, but it did so silently. This could hide underlying issues, such as invalid markdown or a problem with the Goldmark library, making the system harder to debug.

**Root Cause:** The original implementation prioritized graceful degradation (showing plain text instead of an error) but overlooked the need for visibility into when that degradation was happening.

**Solution:** I added a single line to log the error using the standard `slog` library whenever the `md.Convert` function fails. This makes the fallback visible in the logs without changing the user-facing behavior.

**Pattern:** Silent fallbacks or error swallowing in core utility functions should be avoided. When a function fails and falls back to a safe or default behavior, it should always log the error. This provides essential visibility for developers and operators to detect and diagnose problems that might otherwise go unnoticed.
## 2024-07-12 - Centralize Secret Sanitization Logic

**Issue:** The secret sanitization logic, including the `secretPatterns` variable and the `sanitizeArgs` function, was duplicated in both `cmd/mise-ci/worker.go` and `internal/sanitize/sanitize.go`.

**Root Cause:** The sanitization logic was likely copied into the worker from the server-side code to be used for logging, but it was never centralized into a shared package.

**Solution:** I removed the duplicated code from `cmd/mise-ci/worker.go` and made it import the `internal/sanitize` package. The worker now calls the centralized `sanitize.SanitizeArgs` function, ensuring there is a single source of truth for secret sanitization.

**Pattern:** Utility functions and their related data structures that are used in multiple places should be centralized in a shared package to avoid code duplication and ensure consistency. This follows the DRY (Don't Repeat Yourself) principle and makes the code easier to maintain.
