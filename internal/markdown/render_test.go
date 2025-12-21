package markdown

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
		notContains string
	}{
		{
			name:     "Bold",
			input:    "**Hello**",
			contains: "<strong>Hello</strong>",
		},
		{
			name:     "Italic",
			input:    "*World*",
			contains: "<em>World</em>",
		},
		{
			name:     "XSS",
			input:    "<script>alert('xss')</script>",
			notContains: "<script>",
		},
		{
			name:     "GFM Strikethrough",
			input:    "~Strike~",
			contains: "<del>Strike</del>",
		},
		{
			name:     "Link",
			input:    "[Google](https://google.com)",
			contains: `<a href="https://google.com" rel="nofollow">Google</a>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Render(tt.input)
			if tt.contains != "" && !strings.Contains(string(got), tt.contains) {
				t.Errorf("Render() = %q, want to contain %q", got, tt.contains)
			}
			if tt.notContains != "" && strings.Contains(string(got), tt.notContains) {
				t.Errorf("Render() = %q, want NOT to contain %q", got, tt.notContains)
			}
		})
	}
}
