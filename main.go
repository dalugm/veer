// Veer provides a full-screen terminal interface for external proxy cores.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/dalugm/veer/privilege"
	"github.com/dalugm/veer/settings"
	"github.com/dalugm/veer/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Veer:", err)
		os.Exit(1)
	}
}

func run() (result error) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) == 4 && os.Args[1] == "--veer-helper" {
		return privilege.Serve(ctx, os.Args[2], os.Args[3])
	}
	if len(os.Args) != 1 {
		return fmt.Errorf("veer is a TUI application; launch it without arguments")
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return fmt.Errorf("open Veer in an interactive terminal")
	}
	path, err := settings.DefaultPath()
	if err != nil {
		return err
	}
	backend := privilege.New()
	defer func() {
		ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		result = errors.Join(result, backend.Stop(ctx))
	}()
	model, err := tui.New(ctx, path, backend)
	if err != nil {
		return err
	}
	defer func() { cancel(); model.Shutdown() }()
	_, err = tea.NewProgram(model, tea.WithContext(ctx)).Run()
	return err
}
