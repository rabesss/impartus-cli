//go:build ignore

// Build-date resolves the timestamp injected by Makefile builds.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/rabesss/impartus-cli/internal/buildinfo"
)

func main() {
	date, err := buildinfo.ResolveDate(os.Getenv("BUILD_DATE"), os.Getenv("SOURCE_DATE_EPOCH"), time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(date)
}
