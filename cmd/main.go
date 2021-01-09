package main

import (
	"os"

	"github.com/chrismellard/jx-pipeline/cmd/app"
)

// Entrypoint for the command
func main() {
	if err := app.Run(nil); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
