package main

import (
	"context"
	"time"

	"easytty/internal/terminal"
)

// maxPasteLineDelayMs caps the per-paste pause chosen in the paste dialog.
const maxPasteLineDelayMs = 2000

// Paste sends multi-line text paced line by line (see terminal.Manager.Paste).
// lineDelayMs < 0 uses the default from the settings. It returns at once;
// progress arrives as "paste:progress" events.
func (a *App) Paste(tabID, text string, lineDelayMs int) error {
	if _, err := a.terms.Conn(tabID); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	if a.pastes[tabID] != nil {
		a.mu.Unlock()
		cancel()
		return terminal.ErrPasteBusy
	}
	a.pastes[tabID] = cancel
	a.mu.Unlock()

	if lineDelayMs < 0 {
		lineDelayMs = a.GetSettings().PasteLineDelay
	}
	lineDelayMs = min(lineDelayMs, maxPasteLineDelayMs)
	opts := terminal.PasteOptions{LineDelay: time.Duration(lineDelayMs) * time.Millisecond}
	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.pastes, tabID)
			a.mu.Unlock()
			cancel()
		}()
		err := a.terms.Paste(ctx, tabID, text, opts, func(p terminal.PasteProgress) { a.emit("paste:progress", p) })
		if err != nil && err != terminal.ErrPasteBusy {
			a.emit("paste:progress", terminal.PasteProgress{ID: tabID, State: "error", Error: err.Error()})
		}
	}()
	return nil
}

// CancelPaste stops a running paced paste.
func (a *App) CancelPaste(tabID string) {
	a.mu.Lock()
	cancel := a.pastes[tabID]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
