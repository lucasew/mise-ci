package version

import (
	_ "embed"
	"strings"
)

//go:embed version.txt
var version string

func Get() string {
	return strings.TrimSpace(version)
}
