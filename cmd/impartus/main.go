package main

import (
	"fmt"
	"os"

	"github.com/rabesss/impartus-cli/internal/cli"
)

var (
	version = "dev"
	date    = ""
)

func main() {
	if err := cli.Execute(version, date); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
