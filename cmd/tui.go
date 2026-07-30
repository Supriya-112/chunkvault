package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Supriya-112/chunkvault/internal/vault"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	faintStyle = lipgloss.NewStyle().Faint(true)
)

// progressEnabled reports whether a live progress view should be shown: stdout
// is a real terminal and the user has not passed --no-progress.
func progressEnabled() bool {
	return !noProgress && term.IsTerminal(int(os.Stdout.Fd()))
}

// runWithProgress runs op in the background while a Bubble Tea progress view
// polls prog on the main goroutine. Ctrl-C (or q) cancels op's context. It
// returns op's error once it finishes.
func runWithProgress(cmd *cobra.Command, title, op string, prog *vault.Progress, run func(context.Context) error) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	m := progressModel{
		title:  title,
		op:     op,
		prog:   prog,
		bar:    progress.New(progress.WithDefaultGradient()),
		start:  time.Now(),
		cancel: cancel,
	}
	p := tea.NewProgram(m, tea.WithOutput(cmd.OutOrStdout()))

	errc := make(chan error, 1)
	go func() {
		err := run(ctx)
		errc <- err
		p.Send(doneMsg{err: err}) // ignored if the program already quit
	}()

	_, perr := p.Run()
	cancel() // if the user quit the view, stop the op too
	opErr := <-errc
	// The operation's result is authoritative: a completed backup/verify must
	// not be reported as failed just because the display had a render error.
	if opErr != nil {
		return opErr
	}
	return perr
}

type tickMsg time.Time
type doneMsg struct{ err error }

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// progressModel renders a live view of a running backup or verify by polling a
// shared *vault.Progress on a timer, so its refresh rate is decoupled from how
// fast the engine emits updates.
type progressModel struct {
	title  string
	op     string // "backup" or "verify"
	prog   *vault.Progress
	bar    progress.Model
	start  time.Time
	cancel context.CancelFunc
	done   bool
	err    error
	final  vault.ProgressSnapshot
}

func (m progressModel) Init() tea.Cmd { return tick() }

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w := msg.Width - 4
		if w > 60 {
			w = 60
		}
		if w < 10 {
			w = 10
		}
		m.bar.Width = w
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.err = context.Canceled
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
	case doneMsg:
		m.done = true
		m.err = msg.err
		m.final = m.prog.Snapshot()
		return m, tea.Quit
	case tickMsg:
		if m.done {
			return m, tea.Quit
		}
		return m, tick()
	}
	return m, nil
}

func (m progressModel) View() string {
	snap := m.prog.Snapshot()
	if m.done {
		snap = m.final
	}
	elapsed := time.Since(m.start).Round(100 * time.Millisecond)

	var stats string
	if m.op == "verify" {
		stats = fmt.Sprintf("%d / %d chunks · %d corrupt · %d missing",
			snap.Done, snap.Total, snap.Corrupt, snap.Missing)
	} else {
		stats = fmt.Sprintf("%s / %s · %d files · %d chunks (%d new) · %s stored",
			humanBytes(snap.Done), humanBytes(snap.Total), snap.Files, snap.Chunks, snap.NewChunks, humanBytes(snap.StoredBytes))
		if snap.Current != "" && !m.done {
			stats += "\n" + faintStyle.Render("→ "+snap.Current)
		}
	}

	return fmt.Sprintf("%s  %s\n%s\n%s\n",
		titleStyle.Render(m.title),
		faintStyle.Render(elapsed.String()),
		m.bar.ViewAs(snap.Fraction()),
		faintStyle.Render(stats),
	)
}
