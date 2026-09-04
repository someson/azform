package update

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// AvailableMsg is sent when a newer azform release is detected.
// Empty Latest means "no update available".
type AvailableMsg struct {
	Latest string
}

// CheckCmd returns a tea.Cmd that performs the update check.
func CheckCmd(opts Options) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		latest, _ := Check(ctx, opts)
		return AvailableMsg{Latest: latest}
	}
}
