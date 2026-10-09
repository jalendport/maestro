package main

import (
	"os"

	"github.com/jalendport/maestro/internal/cli"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Execute(version))
}
