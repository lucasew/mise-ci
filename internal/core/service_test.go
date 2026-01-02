package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeArgs(t *testing.T) {
	// Create a dummy service, as sanitizeArgs is a method on it.
	s := &Service{}

	testCases := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "No secrets",
			input:    []string{"command", "--flag", "value"},
			expected: []string{"command", "--flag", "value"},
		},
		{
			name:     "URL with password",
			input:    []string{"https://user:password123@example.com"},
			expected: []string{"https://user:%5BREDACTED%5D@example.com"},
		},
		{
			name:     "GitHub Token",
			input:    []string{"-t", "ghp_1234567890abcdef1234567890abcdef1234"},
			expected: []string{"-t", "[REDACTED]"},
		},
		{
			name:     "GitLab Token",
			input:    []string{"--token", "glpat-abcdefghijklmnopqrstuvwxyz"},
			expected: []string{"--token", "[REDACTED]"},
		},
		{
			name:     "Authorization Header with JWT",
			input:    []string{"Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"},
			expected: []string{"Authorization: Bearer [REDACTED]"},
		},
		{
			name:     "Authorization Header Plain Token",
			input:    []string{"Authorization: token ghp_1234567890abcdef1234567890abcdef1234"},
			expected: []string{"Authorization: token [REDACTED]"},
		},
		{
			name:     "API Key in header format",
			input:    []string{"api-key: some-very-long-and-secure-api-key-string"},
			expected: []string{"api-key: [REDACTED]"},
		},
		{
			name:     "Password in assignment format",
			input:    []string{"password=\"another-super-secret-password-value\""},
			expected: []string{"password=\"[REDACTED]\""},
		},
		{
			name:     "Mixed secrets",
			input:    []string{"-url", "https://user:pass@host.com", "key=a-very-long-secret-key-that-should-be-redacted", "someothervalue"},
			expected: []string{"-url", "https://user:%5BREDACTED%5D@host.com", "key=[REDACTED]", "someothervalue"},
		},
		{
			name:     "Short value that should not be redacted",
			input:    []string{"key=short"},
			expected: []string{"key=short"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sanitized := s.sanitizeArgs(tc.input)
			assert.Equal(t, tc.expected, sanitized)
		})
	}
}
