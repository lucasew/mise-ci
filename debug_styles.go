package main

import (
	"fmt"
	"github.com/buildkite/terminal-to-html/v3"
)

func main() {
	// Test Bold
	fmt.Printf("Bold (1): %s\n", terminal.Render([]byte("\x1b[1mBold\x1b[0m")))
	// Test Dim
	fmt.Printf("Dim (2): %s\n", terminal.Render([]byte("\x1b[2mDim\x1b[0m")))
    // Test Underline
	fmt.Printf("Underline (4): %s\n", terminal.Render([]byte("\x1b[4mUnderline\x1b[0m")))
}
