package sanitize

import (
	"net/url"
	"regexp"
)

type SecretPattern struct {
	Pattern     *regexp.Regexp
	Replacement string
}

// SecretPatterns is a list of compiled regular expressions and their corresponding replacements for detecting secrets.
var SecretPatterns = []SecretPattern{
	{
		// Redact value in 'Authorization: Bearer <token>' or 'Authorization: token <token>'
		Pattern:     regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|token)\s+)\S+`),
		Replacement: `${1}[REDACTED]`,
	},
	{
		// Redact common key formats like 'api-key: value' or 'password = "value"'
		Pattern:     regexp.MustCompile(`(?i)((?:api-key|token|secret|password|key)(?:[\s=:]*['"]?))([a-zA-Z0-9_.-]{20,})(['"]?)`),
		Replacement: `${1}[REDACTED]${3}`,
	},
	{
		// Redact specific token patterns
		Pattern:     regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
		Replacement: "[REDACTED]",
	},
	{
		Pattern:     regexp.MustCompile(`glpat-[a-zA-Z0-9_-]{20,}`),
		Replacement: "[REDACTED]",
	},
	{
		// Redact JWTs
		Pattern:     regexp.MustCompile(`ey[J-Za-z0-9-_=]+\.[J-Za-z0-9-_=]+\.[J-Za-z0-9-_.+/=]*`),
		Replacement: "[REDACTED]",
	},
}

func SanitizeArgs(args []string) []string {
	sanitized := make([]string, len(args))
	for i, arg := range args {
		sanitizedArg := arg

		// 1. Handle well-formed URLs first, as it's the most robust method.
		if u, err := url.Parse(sanitizedArg); err == nil && u.User != nil {
			if _, isSet := u.User.Password(); isSet {
				u.User = url.UserPassword(u.User.Username(), "[REDACTED]")
				sanitized[i] = u.String()
				continue // Argument sanitized, move to the next one.
			}
		}

		// 2. If not a URL with a password, apply regex for other common secret patterns.
		for _, p := range SecretPatterns {
			sanitizedArg = p.Pattern.ReplaceAllString(sanitizedArg, p.Replacement)
		}
		sanitized[i] = sanitizedArg
	}
	return sanitized
}
