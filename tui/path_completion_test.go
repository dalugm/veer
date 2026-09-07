package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPathCandidatesDirectoriesAndFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "alpine", "space folder", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "asset.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	prefix := root + string(os.PathSeparator)
	got, err := completePaths(t.Context(), prefix+"a", false)
	if err != nil ||
		!slices.Equal(
			got,
			[]string{
				prefix + "alpha" + string(os.PathSeparator),
				prefix + "alpine" + string(os.PathSeparator),
			},
		) {
		t.Fatalf("directories: %v %v", got, err)
	}
	got, err = completePaths(t.Context(), prefix+"a", true)
	if err != nil || !slices.Contains(got, prefix+"asset.json") {
		t.Fatalf("file missing: %v %v", got, err)
	}
	got, err = completePaths(t.Context(), prefix+"space", false)
	if err != nil || len(got) != 1 || !strings.Contains(got[0], "space folder") {
		t.Fatal("spaces not preserved")
	}
	got, err = completePaths(t.Context(), prefix, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range got {
		if strings.Contains(path, ".hidden") {
			t.Fatal("unrequested dot directory")
		}
	}
	got, err = completePaths(t.Context(), prefix+".", false)
	if err != nil || len(got) != 1 {
		t.Fatal("explicit hidden path not completed")
	}
	got, err = completePaths(t.Context(), "~", false)
	if err != nil || !slices.Equal(got, []string{"~" + string(os.PathSeparator)}) {
		t.Fatal("tilde not completed")
	}
	t.Chdir(root)
	got, err = completePaths(t.Context(), "ass", true)
	if err != nil || !slices.Equal(got, []string{"." + string(os.PathSeparator) + "asset.json"}) {
		t.Fatalf("relative file path: %v %v", got, err)
	}
	if _, err = completePaths(
		t.Context(),
		prefix+"missing"+string(os.PathSeparator),
		true,
	); err == nil {
		t.Fatal("expected unreadable parent error")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = completePaths(ctx, prefix, true); err == nil {
		t.Fatal("cancelled read continued")
	}
}

func finishPathQuery(t *testing.T, m *Model) {
	t.Helper()
	query := m.requestPathCompletion()
	if query == nil {
		t.Fatal("missing completion query")
	}
	_, read := m.Update(query())
	if read == nil {
		t.Fatal("missing asynchronous read")
	}
	m.Update(read())
}

func TestPathCompletionAllPathFields(t *testing.T) {
	for _, tc := range []struct {
		kind  string
		field int
		files bool
	}{{"geo", 0, false}, {"import", 1, true}, {"settings", 0, true}, {"settings", 1, false}} {
		t.Run(tc.kind+string(rune('0'+tc.field)), func(t *testing.T) {
			m := newTestModel(t)
			switch tc.kind {
			case "geo":
				m.openGeo()
			case "import":
				m.openImport()
			case "settings":
				m.openSettings()
			}
			m.form.focus = tc.field
			m.form.inputs[tc.field].SetValue("/base/a")
			m.form.inputs[tc.field].CursorEnd()
			called := false
			m.readPaths = func(_ context.Context, text string, files bool) ([]string, error) {
				called = true
				if text != "/base/a" || files != tc.files {
					t.Fatal("wrong completion request")
				}
				return []string{"/base/alpha/", "/base/alpine/"}, nil
			}
			finishPathQuery(t, m)
			if !called {
				t.Fatal("path not read")
			}
			m.width, m.height = 60, 18
			if !strings.Contains(ansi.Strip(m.View().Content), "alpha") {
				t.Fatal("candidate not visible in small window")
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			if m.form.inputs[tc.field].Value() != "/base/alpine/" || m.form.focus != tc.field {
				t.Fatal("Tab did not complete selected directory")
			}
			// Enter always moves on (or submits a last field) without requiring a match.
			if tc.field < len(m.form.inputs)-1 {
				m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				if m.form.focus == tc.field {
					t.Fatal("cannot leave directory completion")
				}
			}
		})
	}
}

func TestPathCompletionIgnoresStaleResultsAndKeepsOneRead(t *testing.T) {
	m := newTestModel(t)
	m.openGeo()
	m.form.inputs[0].SetValue("old/")
	m.readPaths = func(context.Context, string, bool) ([]string, error) { return []string{"old/child/"}, nil }
	_, read := m.Update(m.requestPathCompletion()())
	m.form.inputs[0].SetValue("new/")
	query := m.requestPathCompletion()
	_, secondRead := m.Update(query())
	if secondRead != nil {
		t.Fatal("overlapping filesystem reads")
	}
	_, retry := m.Update(read())
	if len(m.form.pathItems) != 0 || retry == nil {
		t.Fatal("old read replaced current candidates")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_, read = m.Update(retry())
	if read != nil {
		t.Fatal("closed form started another read")
	}
}

func TestNoPathCandidatesPreservesTabNavigation(t *testing.T) {
	m := newTestModel(t)
	m.openGeo()
	m.form.inputs[0].SetValue("new/not-created")
	m.readPaths = func(context.Context, string, bool) ([]string, error) { return nil, os.ErrPermission }
	finishPathQuery(t, m)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.form.focus != 1 || m.form.inputs[0].Value() != "new/not-created" {
		t.Fatal("new or unreadable path was rejected")
	}
}

func TestTabWhileCompletionPending(t *testing.T) {
	m := newTestModel(t)
	m.openGeo()
	m.readPaths = func(context.Context, string, bool) ([]string, error) { return []string{"folder/"}, nil }
	_, read := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if read == nil || m.form.focus != 0 {
		t.Fatal("Tab skipped pending candidates")
	}
	m.Update(read())
	if m.form.inputs[0].Value() != "folder/" {
		t.Fatal("pending Tab did not accept result")
	}
}
