// Command readmehtml writes the read me as a standalone HTML page (the same page the Documents tab
// shows), for the release folder: go run ./cmd/readmehtml <out.html>
package main

import (
	"fmt"
	"os"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/docpages"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: readmehtml <out.html>")
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[1], []byte(docpages.DocumentPage(0, false)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
