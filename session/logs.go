package session

import (
	"strings"
	"sync"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Log appends sanitized output to the bounded session log.
func (c *Controller) Log(s string) {
	s = ansi.Strip(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return -1
		}
		return r
	}, s)
	if len(s) > 4096 {
		s = s[:4096] + "…"
	}
	if s == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot.Logs = append(c.snapshot.Logs, s)
	if len(c.snapshot.Logs) > 400 {
		c.snapshot.Logs = append([]string(nil), c.snapshot.Logs[len(c.snapshot.Logs)-400:]...)
	}
}

type logWriter struct {
	mu         sync.Mutex
	controller *Controller
	pending    string
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(p)
	for {
		idx := strings.IndexByte(w.pending, '\n')
		if idx < 0 {
			break
		}
		w.controller.Log(w.pending[:idx])
		w.pending = w.pending[idx+1:]
	}
	if len(w.pending) > 8192 {
		w.controller.Log(w.pending[:4096])
		w.pending = ""
	}
	return len(p), nil
}

func (w *logWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.controller.Log(w.pending)
	w.pending = ""
}
