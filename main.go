package main

import (
	_ "embed"
	"os"

	"github.com/jalendport/maestro/internal/cli"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

// skill is compiled in so Maestro can refresh it without a source checkout.
//
//go:embed skills/maestro/SKILL.md
var skill string

func main() {
	cli.SkillMD = skill
	os.Exit(cli.Execute(version))
}
