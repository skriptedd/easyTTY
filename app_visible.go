package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"easytty/internal/automation"
	"easytty/internal/fsutil"
	"easytty/internal/store"
	"easytty/internal/terminal"
)

// Events that let the backend drive tabs the user can watch.
const (
	EventTabOpen  = "tab:open"  // {tabId, sessionId}: open a tab with this id
	EventTabFocus = "tab:focus" // tabId: bring an open tab to front
	EventTabAI    = "tab:ai"    // {tabId, client, active}: banner "KI tippt …"
	EventTabClose = "tab:close" // tabId: close a tab the backend opened in vain
)

const (
	visibleConnectTimeout = 90 * time.Second // the user may have to log in first
	visiblePoll           = 200 * time.Millisecond
)

// connectedTab returns an open, connected tab of the session ("" = none).
func (a *App) connectedTab(sessionID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, m := range a.tabMeta {
		if m.sessionID != sessionID {
			continue
		}
		if _, err := a.terms.Conn(id); err == nil {
			return id
		}
	}
	return ""
}

// waitConnected waits until the UI has opened and connected tabID.
func (a *App) waitConnected(ctx context.Context, tabID string) error {
	deadline := time.NewTimer(visibleConnectTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(visiblePoll)
	defer tick.Stop()
	for {
		if _, err := a.terms.Conn(tabID); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Tab wurde nicht verbunden (Anmeldung abgebrochen oder Gerät nicht erreichbar)")
		case <-tick.C:
		}
	}
}

// aiStream is a tab stream that clears the "KI tippt" banner when done.
type aiStream struct {
	*terminal.TabStream
	done func()
}

func (s aiStream) Close() error {
	err := s.TabStream.Close()
	s.done()
	return err
}

// visibleOpener runs automation in tabs the user sees: an open tab of the
// session is reused (for serial consoles the only way anyway), otherwise
// the UI opens one. key is what CancelAI cancels.
func (a *App) visibleOpener(client, key string) automation.Opener {
	return func(ctx context.Context, sessionID string) (automation.Target, error) {
		s, err := a.store.Session(sessionID)
		if err != nil {
			return automation.Target{}, err
		}
		if s.Protocol == store.ProtocolRDP {
			return automation.Target{}, fmt.Errorf("%s: In einen Remotedesktop lassen sich keine Befehle tippen", s.Name)
		}
		tabID := a.connectedTab(sessionID)
		if tabID == "" {
			tabID = "k" + fsutil.NewID()
			a.emit(EventTabOpen, map[string]string{"tabId": tabID, "sessionId": sessionID})
			if err := a.waitConnected(ctx, tabID); err != nil {
				a.emit(EventTabClose, tabID) // no half-open tab with a stale login prompt
				return automation.Target{}, fmt.Errorf("%s: %w", s.Name, err)
			}
		} else {
			a.emit(EventTabFocus, tabID)
		}
		stream, err := a.terms.Tap(tabID)
		for wait := 0; errors.Is(err, terminal.ErrTapBusy) && wait < int(serialLoginWindow/time.Second); wait++ {
			time.Sleep(time.Second) // a console auto-login is still running
			stream, err = a.terms.Tap(tabID)
		}
		if err != nil {
			return automation.Target{}, err
		}
		a.mu.Lock()
		a.aiTabs[tabID] = key
		a.mu.Unlock()
		a.emit(EventTabAI, map[string]any{"tabId": tabID, "client": client, "active": true})
		done := func() {
			a.mu.Lock()
			delete(a.aiTabs, tabID)
			a.mu.Unlock()
			a.emit(EventTabAI, map[string]any{"tabId": tabID, "client": client, "active": false})
		}
		pw, _ := a.store.Password(s.ID)
		conn, err := loginIfNeeded(ctx, aiStream{TabStream: stream, done: done}, s, pw)
		if err != nil {
			conn.Close()
			return automation.Target{}, err
		}
		return automation.Target{Conn: conn, Name: s.Name}, nil
	}
}

// readable applies the typing speed from the settings to visible runs.
func (a *App) readable(o *automation.Options) {
	switch a.GetSettings().TypeSpeed {
	case "fast":
		o.TypeDelay, o.StepPause = 0, 0
	case "slow":
		o.TypeDelay, o.StepPause = 80*time.Millisecond, 2*time.Second
	default:
		o.TypeDelay, o.StepPause = 35*time.Millisecond, 900*time.Millisecond
	}
}

// CancelAI stops the automation typing into a tab (button in the banner).
func (a *App) CancelAI(tabID string) bool {
	a.mu.Lock()
	key := a.aiTabs[tabID]
	a.mu.Unlock()
	e, err := a.engine()
	return key != "" && err == nil && e.Cancel(key)
}
