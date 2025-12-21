package markdown

import (
	"bytes"
	"html/template"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)
	policy = bluemonday.UGCPolicy()
)

// Render converts markdown source to sanitized HTML.
func Render(source string) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		// Fallback to plain text if markdown rendering fails
		return template.HTML(template.HTMLEscapeString(source))
	}

	sanitized := policy.SanitizeBytes(buf.Bytes())
	return template.HTML(sanitized)
}
