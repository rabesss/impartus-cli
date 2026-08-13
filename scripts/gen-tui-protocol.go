//go:build ignore

// Protocol generator - projects internal/tuiproto/protocol.schema.json into the
// generated Go DTOs and the generated TypeScript definitions used by the
// experimental OpenTUI helper. Run from the repository root:
//
//	go run scripts/gen-tui-protocol.go
//
// internal/tuiproto's drift test fails when the checked-in output does not
// match what this generator produces.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	document, err := tuiproto.LoadDocument()
	if err != nil {
		return err
	}
	goSource, err := tuiproto.RenderGo(document)
	if err != nil {
		return err
	}
	typeScriptSource, err := tuiproto.RenderTypeScript(document)
	if err != nil {
		return err
	}
	for path, content := range map[string][]byte{
		tuiproto.GoOutputPath:         goSource,
		tuiproto.TypeScriptOutputPath: typeScriptSource,
	} {
		if err := writeGenerated(path, content); err != nil {
			return err
		}
	}
	return nil
}

func writeGenerated(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare generated output directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write generated %s: %w", path, err)
	}
	fmt.Println("generated", path)
	return nil
}
