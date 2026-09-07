package tui

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/dalugm/veer/settings"
)

type (
	pathQueryMsg struct {
		form  *form
		seq   uint64
		text  string
		files bool
	}
	pathResultMsg struct {
		query pathQueryMsg
		items []string
	}
)

// Directory reads are read-only and run outside Update/View. At most one may
// be in flight: cancellation cannot interrupt every network-filesystem syscall.
// They do not join the mutation-job fence, so a slow mount cannot hold up exit.
func completePaths(ctx context.Context, input string, files bool) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == "~" {
		return []string{"~" + string(os.PathSeparator)}, nil
	}
	dir, prefix := filepath.Split(input)
	lookup := settings.ExpandPath(dir)
	if lookup == "" {
		lookup = "."
	}
	entries, err := os.ReadDir(lookup)
	if err != nil {
		return nil, err
	}
	separator := string(os.PathSeparator)
	if strings.HasSuffix(dir, "/") {
		separator = "/"
	}
	var matches []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if safe(name) != name {
			continue
		}
		compared, wanted := name, prefix
		if runtime.GOOS == "windows" {
			compared, wanted = strings.ToLower(name), strings.ToLower(prefix)
		}
		if !strings.HasPrefix(compared, wanted) ||
			(strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		isDir, regular := entry.IsDir(), entry.Type().IsRegular()
		if entry.Type()&os.ModeSymlink != 0 {
			info, statErr := os.Stat(filepath.Join(lookup, name))
			isDir = statErr == nil && info.IsDir()
			regular = statErr == nil && info.Mode().IsRegular()
		}
		if isDir {
			matches = append(matches, dir+name+separator)
		} else if files {
			if regular {
				prefixDir := dir
				if prefixDir == "" {
					prefixDir = "." + separator
				}
				matches = append(matches, prefixDir+name)
			}
		}
		if len(matches) == 100 {
			break
		}
	}
	slices.Sort(matches)
	return matches, nil
}

func (f *form) pathField() bool {
	return (f.kind == "geo" && f.focus == 0) || (f.kind == "import" && f.focus == 1) ||
		(f.kind == "settings" && (f.focus == 0 || f.focus == 1))
}

func (f *form) pathFiles() bool { return f.kind == "import" || (f.kind == "settings" && f.focus == 0) }

func (m *Model) clearPathCompletion() {
	if m.pathCancel != nil {
		m.pathCancel()
		m.pathCancel = nil
	}
	if m.form != nil {
		m.form.pathSeq++
		m.form.pathItems = nil
		m.form.pathIndex = 0
		m.form.pathLoading = false
		m.form.pathAccept = false
	}
}

func (m *Model) requestPathCompletion() tea.Cmd {
	m.clearPathCompletion()
	f := m.form
	if f == nil || !f.pathField() || m.busy {
		return nil
	}
	text := f.inputs[f.focus].Value()
	if f.inputs[f.focus].Position() != utf8.RuneCountInString(text) {
		return nil
	}
	f.pathLoading = true
	query := pathQueryMsg{f, f.pathSeq, text, f.pathFiles()}
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return query })
}

func (m *Model) pathQueryCurrent(query pathQueryMsg) bool {
	return m.form == query.form && m.form != nil && m.form.pathField() && !m.busy &&
		m.form.pathSeq == query.seq
}

func (m *Model) readPath(query pathQueryMsg) tea.Cmd {
	if !m.pathQueryCurrent(query) || m.pathReading {
		return nil
	}
	m.pathReading = true
	ctx, cancel := context.WithTimeout(m.ctx, time.Second)
	m.pathCancel = cancel
	read := m.readPaths
	return func() tea.Msg {
		defer cancel()
		items, err := read(ctx, query.text, query.files)
		if err != nil || ctx.Err() != nil {
			items = nil
		}
		return pathResultMsg{query, items}
	}
}

func (m *Model) pathResult(result pathResultMsg) tea.Cmd {
	m.pathReading = false
	m.pathCancel = nil
	if m.pathQueryCurrent(result.query) {
		m.form.pathItems = result.items
		m.form.pathIndex = 0
		m.form.pathLoading = false
		if m.form.pathAccept {
			m.form.pathAccept = false
			return m.updateForm(tea.KeyPressMsg{Code: tea.KeyTab})
		}
		return nil
	}
	return m.requestPathCompletion()
}

func (m *Model) pathKey(key string) (bool, tea.Cmd) {
	f := m.form
	if f.pathField() && f.pathLoading && key == "tab" {
		f.pathAccept = true
		return true, m.readPath(
			pathQueryMsg{f, f.pathSeq, f.inputs[f.focus].Value(), f.pathFiles()},
		)
	}
	if !f.pathField() || len(f.pathItems) == 0 {
		return false, nil
	}
	switch key {
	case "down":
		f.pathIndex = (f.pathIndex + 1) % len(f.pathItems)
		return true, nil
	case "up":
		f.pathIndex = (f.pathIndex + len(f.pathItems) - 1) % len(f.pathItems)
		return true, nil
	case "tab":
		f.inputs[f.focus].SetValue(f.pathItems[f.pathIndex])
		f.inputs[f.focus].CursorEnd()
		if strings.HasSuffix(f.inputs[f.focus].Value(), string(os.PathSeparator)) ||
			strings.HasSuffix(f.inputs[f.focus].Value(), "/") {
			return true, m.requestPathCompletion()
		}
		m.clearPathCompletion()
		return true, nil
	}
	return false, nil
}

func (m *Model) pathCompletionView(w, h int) string {
	f := m.form
	lines := []string{accent.Render(f.labels[f.focus]), f.inputs[f.focus].View()}
	hint := "Type a path · Enter continues"
	if f.pathLoading {
		hint = "Looking for paths… · Enter continues"
	} else if len(f.pathItems) > 0 {
		hint = "↑/↓ choose · Tab complete · Enter continues"
	}
	lines = append(lines, faint.Render(clip(hint, w-4)))
	count := max(1, min(5, h-7))
	start := max(0, f.pathIndex-count+1)
	for i := start; i < min(len(f.pathItems), start+count); i++ {
		candidate := f.pathItems[i]
		name := filepath.Base(strings.TrimRight(candidate, `/\`))
		if strings.HasSuffix(candidate, "/") ||
			strings.HasSuffix(candidate, string(os.PathSeparator)) {
			name += string(os.PathSeparator)
		}
		label := "  " + safe(name)
		style := faint
		if i == f.pathIndex {
			label = "› " + safe(name)
			style = accent
		}
		lines = append(lines, style.Render(clip(label, w-4)))
	}
	return box(f.title, strings.Join(lines, "\n"), w, h)
}
