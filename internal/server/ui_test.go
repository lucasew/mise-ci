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
	s := NewUIServer(nil, logger)

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
