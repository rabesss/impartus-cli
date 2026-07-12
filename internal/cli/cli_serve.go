package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/rabesss/impartus-cli/internal/server"
)

func runServe(args []string, _ string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 8080, "Port")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("serve does not accept positional arguments")
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", *port)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	srv := server.NewAPIServerWithPersistence(strconv.Itoa(*port), cfg, "")
	return srv.Start(context.Background())
}

func parseServePort(args []string) (int, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 8080, "Port")
	if err := fs.Parse(args); err != nil {
		return 0, err
	}
	if fs.NArg() > 0 {
		return 0, fmt.Errorf("serve does not accept positional arguments")
	}
	if *port < 1 || *port > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535, got %d", *port)
	}
	return *port, nil
}
