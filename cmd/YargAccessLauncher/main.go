// Command YargAccessLauncher is the YARG Access launcher: it installs YARG, the accessibility mod and
// music for screen-reader users.
package main

import (
	"log"
	"os"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/config"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/ui"
)

func main() {
	os.MkdirAll(config.Dir, 0o755)
	if f, err := os.Create(config.LogPath); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	log.SetFlags(log.Ldate | log.Ltime)
	if err := ui.Run(); err != nil {
		log.Fatal(err)
	}
}
