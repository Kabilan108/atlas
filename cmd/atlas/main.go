package main

import (
	"os"

	"github.com/kabilan108/atlas/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Execute(version))
}
