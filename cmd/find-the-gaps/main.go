package main

import (
	"os"

	"github.com/sandgardenhq/find-the-gaps/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
