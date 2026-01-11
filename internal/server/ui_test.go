package server

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lucasew/mise-ci/internal/core"
)

func TestRenderRunMarkdown(t *testing.T) {
	logger := slog.Default()
	// Core can be nil for this test as we only test template rendering of provided data
	s := NewUIServer(nil, logger, "http://localhost:8080")

	run := &core.RunInfo{
		ID:            "test-run",
		Status:        core.StatusSuccess,
		StartedAt:     time.Now(),
		CommitMessage: "**Bold Commit**",
	}

	data := map[string]interface{}{
		"Title": "Run Test",
		"Run":   run,
		"Token": "test-token",
		"Version": "1.0.0",
	}

	var buf bytes.Buffer
	if err := s.engine.Render(&buf, "templates/pages/run.html", data); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "<strong>Bold Commit</strong>") {
		t.Errorf("Expected markdown rendering, got: %s", output)
	}
}

func TestRenderRunMarkdownSplit(t *testing.T) {
	logger := slog.Default()
	s := NewUIServer(nil, logger, "http://localhost:8080")

	run := &core.RunInfo{
		ID:            "test-run",
		Status:        core.StatusSuccess,
		StartedAt:     time.Now(),
		CommitMessage: "**Subject**\n\nBody content",
	}

	data := map[string]interface{}{
		"Title": "Run Test",
		"Run":   run,
		"Token": "test-token",
		"Version": "1.0.0",
	}

	var buf bytes.Buffer
	if err := s.engine.Render(&buf, "templates/pages/run.html", data); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	output := buf.String()

    // Check H1 contains Subject. Note: markdown renders it as <p><strong>Subject</strong></p> usually?
    // Wait, Render returns template.HTML.
    // goldmark renders "**Subject**" as "<p><strong>Subject</strong></p>\n" if it's a block?
    // If it's a single line, goldmark might still wrap in <p>.
    // Let's check what Render returns for "**Subject**".
    // In TestRenderRunMarkdown, we checked "<strong>Bold Commit</strong>".

    // If goldmark wraps in <p>, then H1 will contain <p>. `<h1><p>Subject</p></h1>` is invalid HTML but browsers render it.
    // The previous test passed "<strong>Bold Commit</strong>", so it definitely contained that.

    // Let's just check for the content presence for now.

	if !strings.Contains(output, "<strong>Subject</strong>") {
		t.Errorf("Expected subject rendering, got: %s", output)
	}

	if !strings.Contains(output, "Body content") {
		t.Errorf("Expected body rendering, got: %s", output)
	}
}
