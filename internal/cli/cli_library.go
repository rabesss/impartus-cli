package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/rabesss/impartus-cli/internal/library"
)

func runLibrary(args []string) error {
	command, data, err := executeLibrary(context.Background(), args)
	if err != nil {
		return err
	}
	switch command {
	case "library.list":
		records, ok := data.([]library.ArtifactRecord)
		if !ok {
			return errors.New("library list returned an unexpected result")
		}
		return printLibraryList(records)
	case "library.show":
		return printIndentedJSON(data)
	case "library.verify":
		verified, ok := data.([]library.Verification)
		if !ok {
			return errors.New("library verify returned an unexpected result")
		}
		return printLibraryVerification(verified)
	default:
		return errors.New("unsupported library command")
	}
}

func executeJSONLibrary(args []string) error {
	command, data, err := executeLibrary(context.Background(), args)
	if err != nil {
		return newJSONError(command, err)
	}
	return emitJSONEnvelope(newSuccessEnvelope(command, data))
}

func executeLibrary(ctx context.Context, args []string) (string, any, error) {
	command := libraryCommandName(args)
	if len(args) == 0 {
		return command, nil, errors.New("library requires list, show, or verify")
	}
	store, err := library.Open(ctx, library.Options{})
	if err != nil {
		return command, nil, err
	}
	defer closeLibraryStore(store)

	switch args[0] {
	case "list":
		if len(args) != 1 {
			return command, nil, errors.New("library list does not accept arguments")
		}
		records, err := store.ListArtifacts(ctx)
		return command, records, err
	case "show":
		if len(args) != 2 {
			return command, nil, errors.New("library show requires exactly one artifact ID")
		}
		record, err := store.GetArtifact(ctx, args[1])
		return command, record, err
	case "verify":
		return executeLibraryVerify(ctx, store, command, args[1:])
	default:
		return command, nil, fmt.Errorf("unknown library command: %s", args[0])
	}
}

func closeLibraryStore(store *library.Store) {
	if err := store.Close(); err != nil {
		return
	}
}

func executeLibraryVerify(ctx context.Context, store *library.Store, command string, args []string) (string, any, error) {
	flags := flag.NewFlagSet("library verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	withHash := flags.Bool("hash", false, "compute or recheck SHA-256")
	if err := flags.Parse(args); err != nil {
		return command, nil, err
	}
	if flags.NArg() > 1 {
		return command, nil, errors.New("library verify accepts at most one artifact ID")
	}
	options := library.VerifyOptions{Hash: *withHash}
	if flags.NArg() == 1 {
		verified, err := store.VerifyArtifact(ctx, flags.Arg(0), options)
		if err != nil {
			return command, nil, err
		}
		return command, []library.Verification{verified}, nil
	}
	verified, err := store.VerifyAll(ctx, options)
	return command, verified, err
}

func libraryCommandName(args []string) string {
	if len(args) == 0 {
		return "library"
	}
	switch args[0] {
	case "list", "show", "verify":
		return "library." + args[0]
	default:
		return "library"
	}
}

func printLibraryList(records []library.ArtifactRecord) error {
	if len(records) == 0 {
		_, err := fmt.Fprintln(os.Stdout, "Library is empty.")
		return err
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "ARTIFACT ID\tLECTURE\tTOPIC\tFILES"); err != nil {
		return err
	}
	for _, record := range records {
		if _, err := fmt.Fprintf(writer, "%s\t%d\t%s\t%d\n", record.Manifest.ArtifactID, record.Manifest.Lecture.SeqNo, record.Manifest.Lecture.Topic, len(record.Files)); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func printIndentedJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printLibraryVerification(results []library.Verification) error {
	for _, result := range results {
		status := "OK"
		if !result.OK {
			status = "PROBLEM"
		}
		if _, err := fmt.Fprintf(os.Stdout, "[%s] %s\n", status, result.ArtifactID); err != nil {
			return err
		}
		for _, file := range result.Files {
			if _, err := fmt.Fprintf(os.Stdout, "  %s  %s\n", file.Status, file.Path); err != nil {
				return err
			}
		}
	}
	return nil
}
