package app

import (
	"context"
	"errors"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"

	minstance "github.com/patrache/masil/internal/instance"
	"github.com/patrache/masil/internal/ui"
)

// Run acquires the TUI lock, builds the tea.Program (alt screen via View),
// starts the realtime bridge, runs, and saves state.json on exit.
func Run(ctx context.Context, o Options) error {
	return runProgram(ctx, New(optionsFromEnv(o)))
}

// optionsFromEnv is o with what Run takes from the process env: MASIL_PANE=1
// (set by termhost for every program it runs in a pane) => NestedPane.
func optionsFromEnv(o Options) Options {
	o.NestedPane = o.NestedPane || os.Getenv("MASIL_PANE") == "1"
	return o
}

// runProgram is Run on an already built model with extra program options (tests:
// input, output, window size). The shared tui.lock is held for the whole run. The
// realtime bridge starts from Init, once m.send is the program's Send. On exit —
// whatever the reason, before the error is mapped — realtime is stopped, state.json
// is written (failure only logged) and the lock released. An outer ctx cancel (a
// signal) is a normal exit: tea's ErrProgramKilled wrapping ctx.Err() becomes nil.
// Any other error (a recovered panic: ErrProgramKilled + ErrProgramPanic) is
// returned as is; tea.Quit already returns nil.
func runProgram(ctx context.Context, m *Model, opts ...tea.ProgramOption) error {
	release, err := minstance.AcquireTUI(m.ctx.Paths)
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, append([]tea.ProgramOption{tea.WithContext(ctx)}, opts...)...)
	m.send = p.Send
	m.queue() // ctx.Post and the realtime bridge deliver through it
	_, err = p.Run()

	// The event loop is over: nothing else touches m now.
	m.stopRealtime()
	m.closeQueue()
	m.send = nil
	if m.creds != nil {
		if werr := writeState(m.ctx.Paths, m.savedState()); werr != nil {
			slog.Warn("save state.json", "err", werr)
		}
	}
	release()

	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil && !errors.Is(err, tea.ErrProgramPanic) {
		return nil
	}
	return err
}

// nestedPaneWarning is the start toast when masil runs inside a masil pane
// (MASIL_PANE=1, v0.1.3 A7.4): the two would fight over keys like ctrl+\. It
// only warns (Init, once); nothing is blocked. Only Run reads the env
// (optionsFromEnv): a model built otherwise (the test harness) needs
// Options.NestedPane.
func (m *Model) nestedPaneWarning() tea.Cmd {
	if !m.opts.NestedPane {
		return nil
	}
	slog.Info("masil started inside a masil pane")
	return ui.Toast(m.ctx.T.Termpane.NestedWarning(), ui.ToastInfo)
}
