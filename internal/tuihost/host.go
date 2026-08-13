// Package tuihost owns the lifecycle boundary between the Go application and
// its compiled OpenTUI child. The child receives presentation data only through
// a private, versioned session and never inherits Impartus credentials.
package tuihost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/rabesss/impartus-cli/internal/secrets"
	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

// Session is the private transport identity required by the child launcher.
type Session interface {
	BaseURL() string
	Capability() string
	ID() string
}

// Options describe one foreground OpenTUI child invocation.
type Options struct {
	Session    Session
	Executable string
	Arguments  []string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
}

// Run launches one OpenTUI child, waits for it, and removes the one-use
// bootstrap on every exit path.
func Run(ctx context.Context, options Options) (returnErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	bootstrap, err := createBootstrap(tuiproto.Bootstrap{
		BaseURL:    options.Session.BaseURL(),
		Capability: options.Session.Capability(),
		Protocol:   tuiproto.ProtocolVersion,
		SessionID:  options.Session.ID(),
	})
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, bootstrap.cleanup())
	}()

	arguments := append(append([]string(nil), options.Arguments...), "--bootstrap", bootstrap.path)
	command := exec.CommandContext(ctx, options.Executable, arguments...) // #nosec G204 -- exact sidecar is resolved by the trusted Go parent
	configureCancellation(command)
	command.Env = childEnvironment(os.Environ())
	command.Stdin = options.Stdin
	command.Stdout = options.Stdout
	command.Stderr = options.Stderr
	if command.Stdin == nil {
		command.Stdin = os.Stdin
	}
	if command.Stdout == nil {
		command.Stdout = os.Stdout
	}
	if command.Stderr == nil {
		command.Stderr = os.Stderr
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start OpenTUI frontend: %w", secrets.SanitizeError(err))
	}
	waitErr := command.Wait()
	consumed, consumeErr := bootstrap.consumed()
	if consumeErr != nil {
		return errors.Join(scrubChildExit(waitErr), consumeErr)
	}
	if !consumed {
		return errors.Join(scrubChildExit(waitErr), errors.New("OpenTUI frontend exited before consuming its private bootstrap"))
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return errors.Join(ctxErr, scrubChildExit(waitErr))
	}
	return scrubChildExit(waitErr)
}

func validateOptions(options Options) error {
	if options.Session == nil {
		return errors.New("OpenTUI session is required")
	}
	if options.Executable == "" {
		return errors.New("OpenTUI executable is required")
	}
	capability := options.Session.Capability()
	if len(capability) < 32 {
		return errors.New("OpenTUI session capability is invalid")
	}
	if options.Session.ID() == "" {
		return errors.New("OpenTUI session identity is required")
	}
	endpoint, err := url.Parse(options.Session.BaseURL())
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("OpenTUI session endpoint is invalid")
	}
	host := net.ParseIP(endpoint.Hostname())
	if host == nil || !host.IsLoopback() || endpoint.Path != tuiproto.ProtocolBasePath {
		return errors.New("OpenTUI session endpoint must be the private loopback protocol root")
	}
	for _, argument := range append([]string{options.Executable}, options.Arguments...) {
		if strings.Contains(argument, capability) {
			return errors.New("OpenTUI child arguments must not contain the session capability")
		}
	}
	return nil
}

func scrubChildExit(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("OpenTUI frontend exited: %w", secrets.SanitizeError(err))
}

func childEnvironment(parent []string) []string {
	allowed := make([]string, 0, 12)
	for _, variable := range parent {
		name, _, found := strings.Cut(variable, "=")
		if !found || !allowedEnvironmentName(name) {
			continue
		}
		allowed = append(allowed, variable)
	}
	return allowed
}

func allowedEnvironmentName(name string) bool {
	if strings.HasPrefix(name, "LC_") {
		return true
	}
	switch strings.ToUpper(name) {
	case "COMSPEC", "COLORTERM", "FORCE_COLOR", "LANG", "NO_COLOR", "PATHEXT",
		"SYSTEMROOT", "TEMP", "TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "TMP", "TMPDIR", "TZ", "WINDIR":
		return true
	default:
		return false
	}
}
