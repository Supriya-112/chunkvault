package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Supriya-112/chunkvault/internal/vault"
)

func newTestModel(op string, cancel func()) progressModel {
	return progressModel{
		title:  "Backing up ./x",
		op:     op,
		prog:   vault.NewProgress(),
		bar:    progress.New(),
		start:  time.Now(),
		cancel: cancel,
	}
}

func TestProgressModelView(t *testing.T) {
	m := newTestModel("backup", nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := updated.(progressModel).View()
	if !strings.Contains(view, "Backing up ./x") {
		t.Fatalf("view is missing the title:\n%s", view)
	}
}

func TestProgressModelDone(t *testing.T) {
	m := newTestModel("backup", nil)
	updated, tc := m.Update(doneMsg{err: nil})
	if !updated.(progressModel).done {
		t.Fatal("doneMsg should mark the model done")
	}
	if tc == nil {
		t.Fatal("doneMsg should return a quit command")
	}
}

func TestProgressModelCancelOnCtrlC(t *testing.T) {
	canceled := false
	m := newTestModel("verify", func() { canceled = true })
	_, tc := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if tc == nil {
		t.Fatal("ctrl+c should return a quit command")
	}
	if !canceled {
		t.Fatal("ctrl+c should cancel the operation")
	}
}
