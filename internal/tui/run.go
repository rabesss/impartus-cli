package tui

import (
	"context"
	"errors"
	"io"

	tea "charm.land/bubbletea/v2"
)

// Run owns the terminal lifecycle for one TUI session. Bubble Tea v2 restores
// the alternate screen on every ordinary quit and program cancellation path.
func Run(ctx context.Context, backend Backend, input io.Reader, output io.Writer, supplied ...Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	options := Options{}
	if len(supplied) > 0 {
		options = supplied[0]
	}
	model := NewWithOptions(ctx, backend, options)
	model.watchLifecycle = true
	program := tea.NewProgram(
		model,
		tea.WithInput(input),
		tea.WithOutput(output),
	)
	return runAndShutdown(model, func() error {
		_, runErr := program.Run()
		return errors.Join(runErr, ctx.Err(), model.runtime.Err())
	})
}

func runAndShutdown(model Model, run func() error) (returnErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			returnErr = errors.Join(returnErr, errors.New("terminal UI panicked unexpectedly"))
		}
		returnErr = errors.Join(returnErr, model.shutdown())
	}()
	return run()
}

func (model Model) shutdown() error {
	if model.cancel != nil {
		model.cancel()
	}
	closeErr := model.playbacks.StopAndClose()
	model.operations.StopAndWait()
	return closeErr
}
