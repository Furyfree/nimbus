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
	return pick(title, items, false)
}

// runPickOne is the single-choice form: the entry under the cursor is the
// answer, and enter confirms it.
func runPickOne(title string, items []pickItem) (string, error) {
	chosen, err := pick(title, items, true)
	if err != nil {
		return "", err
	}
	if len(chosen) != 1 {
		return "", errors.New("nothing chosen")
	}
	return chosen[0], nil
}

func pick(title string, items []pickItem, single bool) ([]string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("the picker needs a terminal; pass explicit IDs instead")
	}
	m := pickerModel{title: title, items: items, selected: map[int]bool{}, single: single}
	for i, it := range items {
		if single && it.Selected {
			m.cursor = i
		}
	}
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
	if single {
		if vis := fm.visible(); len(vis) > 0 && fm.cursor < len(vis) {
			return []string{fm.items[vis[fm.cursor]].ID}, nil
		}
		return nil, errors.New("nothing chosen")
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
	single   bool // one answer, the entry under the cursor
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
		if len(vis) > 0 && !m.single {
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
	if m.single {
		hint = "type to filter, enter chooses, esc cancels"
	}
	fmt.Fprintf(&b, "%s  %s\n", titleStyle.Render(m.title), dimStyle.Render(hint))
	if m.filter != "" {
		fmt.Fprintf(&b, "filter: %s\n", m.filter)
	}
	vis := m.visible()
	start := 0
	if m.cursor >= 20 {
		start = m.cursor - 19
	}
	for n, i := range vis {
		if n < start || n >= start+20 {
			continue
		}
		mark := "[ ]"
		if m.selected[i] {
			mark = "[x]"
		}
		if m.single {
			mark = " "
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
