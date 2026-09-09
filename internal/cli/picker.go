package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// runPicker opens the keyboard-driven multi-select picker on the terminal.
// It is the presentation half of a selection command; the command itself
// still shows the diff and plan and asks for approval afterwards.
func runPicker(title string, items []pickItem) ([]string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("the picker needs a terminal; pass explicit IDs instead")
	}
	m := pickerModel{title: title, items: items, selected: map[int]bool{}}
	for i, it := range items {
		if it.Selected {
			m.selected[i] = true
		}
	}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, err
	}
	fm := final.(pickerModel)
	if fm.aborted {
		return nil, errors.New("picker cancelled")
	}
	var chosen []string
	for i, it := range fm.items {
		if fm.selected[i] {
			chosen = append(chosen, it.ID)
		}
	}
	return chosen, nil
}

type pickerModel struct {
	title    string
	items    []pickItem
	selected map[int]bool
	cursor   int
	filter   string
	aborted  bool
	done     bool
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) visible() []int {
	var idx []int
	for i, it := range m.items {
		if m.filter == "" || strings.Contains(it.ID, m.filter) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	vis := m.visible()
	switch key.String() {
	case "ctrl+c", "esc":
		m.aborted = true
		return m, tea.Quit
	case "enter":
		m.done = true
		return m, tea.Quit
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "ctrl+n":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
	case " ", "tab":
		if len(vis) > 0 {
			i := vis[m.cursor]
			m.selected[i] = !m.selected[i]
		}
	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.cursor = 0
		}
	default:
		if len(key.Runes) == 1 && key.Runes[0] > ' ' {
			m.filter += string(key.Runes)
			m.cursor = 0
		}
	}
	return m, nil
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	cursorStyle = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
)

func (m pickerModel) View() string {
	var b strings.Builder
	hint := "type to filter, space toggles, enter confirms, esc cancels"
	fmt.Fprintf(&b, "%s  %s\n", titleStyle.Render(m.title), dimStyle.Render(hint))
	if m.filter != "" {
		fmt.Fprintf(&b, "filter: %s\n", m.filter)
	}
	vis := m.visible()
	start := max(0, m.cursor-19)
	for n, i := range vis {
		if n < start || n >= start+20 {
			continue
		}
		mark := "[ ]"
		if m.selected[i] {
			mark = "[x]"
		}
		line := fmt.Sprintf("%s %s", mark, m.items[i].ID)
		if m.items[i].Detail != "" {
			line += "  " + dimStyle.Render(m.items[i].Detail)
		}
		if n == m.cursor {
			line = cursorStyle.Render("> " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	if len(vis) > 20 {
		fmt.Fprintf(&b, "%s\n", dimStyle.Render(fmt.Sprintf("%d of %d shown", min(20, len(vis)), len(vis))))
	}
	return b.String()
}
