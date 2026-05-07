// Package main is the entrypoint for the impartus CLI binary.
package main

import (
	"fmt"
	"os"

	"github.com/rabesss/impartus-cli/internal/buildinfo"
	"github.com/rabesss/impartus-cli/internal/cli"
)

func main() {
	if err := cli.Execute(buildinfo.Version, buildinfo.Date); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
