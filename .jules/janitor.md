# Janitor's Journal

This journal is for CRITICAL, reusable learnings only. See prompt for what to record.
## 2026-01-10 - Add Logging to Markdown Rendering Fallback

**Issue:** The `markdown.Render` function had a fallback mechanism to return plain text if the markdown conversion failed, but it did so silently. This could hide underlying issues, such as invalid markdown or a problem with the Goldmark library, making the system harder to debug.

**Root Cause:** The original implementation prioritized graceful degradation (showing plain text instead of an error) but overlooked the need for visibility into when that degradation was happening.

**Solution:** I added a single line to log the error using the standard `slog` library whenever the `md.Convert` function fails. This makes the fallback visible in the logs without changing the user-facing behavior.

**Pattern:** Silent fallbacks or error swallowing in core utility functions should be avoided. When a function fails and falls back to a safe or default behavior, it should always log the error. This provides essential visibility for developers and operators to detect and diagnose problems that might otherwise go unnoticed.
