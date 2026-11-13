package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"easytty/internal/conn"
	"easytty/internal/store"
	"easytty/internal/terminal"
)

// Connect opens a saved session in tab tabID (the UI picks the id so it can
// subscribe to output before any data arrives). Progress arrives as term:state.
func (a *App) Connect(tabID, sessionID string, cols, rows int) error {
	s, err := a.store.Session(sessionID)
	if err != nil {
		return err
	}
	_ = a.store.Touch(sessionID)
	a.startDial(tabID, s, cols, rows)
	return nil
}

// QuickConnect parses "user@host:port" and connects without saving a session.
func (a *App) QuickConnect(tabID, target string, cols, rows int) (conn.Target, error) {
	t, err := conn.ParseTarget(target)
	if err != nil {
		return conn.Target{}, err
	}
	s := store.Session{Name: t.Host, Protocol: store.ProtocolSSH, Host: t.Host, Port: t.Port, User: t.User, Auth: store.AuthAuto}
	a.startDial(tabID, s.Normalize(), cols, rows)
	return t, nil
}

// dial is one pending connection attempt; compared by pointer so a newer
// attempt for the same tab is never cleaned up by an older one.
type dial struct{ cancel context.CancelFunc }

func (a *App) startDial(tabID string, s store.Session, cols, rows int) {
	ctx, cancel := context.WithCancel(a.ctx)
	d := &dial{cancel: cancel}
	a.mu.Lock()
	if prev := a.dials[tabID]; prev != nil {
		prev.cancel()
	}
	a.dials[tabID] = d
	a.mu.Unlock()

	a.emit(terminal.EventState, terminal.State{ID: tabID, State: "connecting", Message: describe(s)})
	go func() {
		defer func() {
			a.mu.Lock()
			if a.dials[tabID] == d {
				delete(a.dials, tabID)
			}
			a.mu.Unlock()
			cancel()
		}()
		if s.Protocol == store.ProtocolSerial {
			_ = a.terms.Close(tabID) // a COM port opens only once: free it before reconnecting
		}
		c, err := a.open(ctx, tabID, s, cols, rows)
		if err != nil {
			if ctx.Err() == nil {
				a.emit(terminal.EventState, terminal.State{ID: tabID, State: "error", Message: err.Error()})
			}
			return
		}
		if ctx.Err() != nil { // tab closed while connecting
			c.Close()
			return
		}
		log, logPath := a.openLog(s)
		a.terms.Attach(tabID, c, log)
		if logPath != "" {
			a.emit("term:log", map[string]string{"id": tabID, "path": logPath})
		}
		if ctx.Err() != nil { // CloseTab raced with Attach
			_ = a.terms.Close(tabID)
			return
		}
		if s.Protocol == store.ProtocolSerial {
			time.Sleep(serialWakeDelay)
			if pw, _ := a.store.Password(s.ID); s.User != "" || pw != "" {
				// The login watcher sends the wake-up Enter itself, so it sees the prompt.
				go a.serialLogin(tabID, s, a.GetSettings().SerialWake)
			} else if a.GetSettings().SerialWake {
				// A console only prints its prompt after a keystroke; send one Enter.
				_ = a.terms.Write(tabID, []byte("\r"))
			}
		}
	}()
}

// serialWakeDelay lets the output pump start before the wake-up Enter is sent.
const serialWakeDelay = 200 * time.Millisecond

func describe(s store.Session) string {
	if s.Protocol == store.ProtocolSerial {
		return s.SerialPort
	}
	if s.User != "" {
		return s.User + "@" + s.Host
	}
	return s.Host
}

func (a *App) open(ctx context.Context, tabID string, s store.Session, cols, rows int) (terminal.Conn, error) {
	switch s.Protocol {
	case store.ProtocolRDP:
		return nil, errors.New("RDP-Sessions öffnen im Remotedesktop, nicht im Terminal")
	case store.ProtocolSerial:
		return conn.OpenSerial(conn.SerialConfig{Port: s.SerialPort, Baud: s.Baud, DataBits: s.DataBits, Parity: s.Parity, StopBits: s.StopBits})
	case store.ProtocolSSH:
		pw := ""
		if s.ID != "" {
			var err error
			if pw, err = a.store.Password(s.ID); err != nil {
				pw = "" // fall back to prompting
			}
		}
		typed := "" // password typed at the prompt; saved once the login worked
		d := conn.Dialer{
			HostKeys:          a.knownHosts.CallbackWith(a.hostKeyConfirm(ctx, tabID, s.Host)),
			HostKeyAlgorithms: a.knownHosts.HostKeyAlgorithms,
			AgentDial:         conn.DialAgent,
			HomeDir:           a.home,
			Ask: func(r conn.PromptRequest) ([]string, bool) {
				ans := a.ask(ctx, PromptEvent{TabID: tabID, Kind: r.Kind, Title: r.Title, Message: r.Message, Fields: r.Fields})
				// Only the target's own password is remembered, never one typed for a jump host.
				if r.Kind == "password" && !r.Jump && ans.ok && len(ans.values) == 1 {
					typed = ans.values[0]
				}
				return ans.values, ans.ok
			},
		}
		cfg, err := withJumps(conn.SSHConfig{
			Host: s.Host, Port: s.Port, User: s.User, Auth: s.Auth, KeyPath: s.KeyPath,
			Password: pw, KeepAlive: time.Duration(s.KeepAlive) * time.Second, Cols: cols, Rows: rows,
		}, s, a.lookupSession)
		if err != nil {
			return nil, err
		}
		c, err := d.Dial(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if s.ID != "" && typed != "" && typed != pw {
			a.rememberPassword(s, typed)
		}
		a.startForwards(c, s)
		return c, nil
	}
	return nil, errors.New("unbekanntes Protokoll")
}

// defaultLogPattern lays logs out as <Logordner>\2026-10-30\core-sw1_140509.log.
const defaultLogPattern = `{date}\{session}_{time}.log`

// openLog starts the transcript for a session: its own LogPath if set,
// otherwise the global automatic logging. Returns nil, "" if logging is off.
func (a *App) openLog(s store.Session) (io.WriteCloser, string) {
	settings := a.GetSettings()
	pattern := s.LogPath
	if pattern == "" {
		if !settings.LogEnabled {
			return nil, ""
		}
		pattern = filepath.Join(settings.LogDir, defaultLogPattern)
	}
	host, target := s.Host, fmt.Sprintf("%s:%d", describe(s), s.Port)
	if s.Protocol == store.ProtocolSerial {
		host, target = s.SerialPort, fmt.Sprintf("%s @ %d Baud", s.SerialPort, s.Baud)
	}
	path := terminal.ExpandLogPath(pattern, s.Name, host, time.Now())
	f, err := terminal.OpenLog(path)
	if err != nil {
		a.emit(terminal.EventNotify, terminal.Notice{Kind: "err", Message: "Session-Log nicht möglich: " + err.Error()})
		return nil, ""
	}
	l, err := terminal.NewSessionLog(f, terminal.LogInfo{Session: s.Name, Target: target}, settings.LogRaw, nil)
	if err != nil {
		f.Close()
		a.emit(terminal.EventNotify, terminal.Notice{Kind: "err", Message: "Session-Log nicht möglich: " + err.Error()})
		return nil, ""
	}
	return l, path
}

// RevealLog shows a log file selected in Explorer.
func (a *App) RevealLog(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return exec.Command(explorerExe(), "/select,", path).Start()
}

// OpenLogDir opens the global log folder in Explorer (creating it if needed).
func (a *App) OpenLogDir() error {
	dir := a.GetSettings().LogDir
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return exec.Command(explorerExe(), dir).Start()
}

// Ack tells the backend how many output bytes the UI has rendered (flow control).
func (a *App) Ack(tabID string, n int) { _ = a.terms.Ack(tabID, n) }

// Write sends keyboard input to a tab.
func (a *App) Write(tabID, data string) error { return a.terms.Write(tabID, []byte(data)) }

// Resize informs the remote side about a new terminal size.
func (a *App) Resize(tabID string, cols, rows int) error { return a.terms.Resize(tabID, cols, rows) }

// SendBreak sends a break signal (serial / SSH).
func (a *App) SendBreak(tabID string) error { return a.terms.Break(tabID) }

// CloseTab disconnects a tab or aborts its pending connection attempt.
func (a *App) CloseTab(tabID string) {
	a.mu.Lock()
	if d := a.dials[tabID]; d != nil {
		d.cancel()
	}
	if cancel := a.pastes[tabID]; cancel != nil {
		cancel()
	}
	delete(a.tabMeta, tabID)
	a.mu.Unlock()
	_ = a.terms.Close(tabID)
}

// rememberPassword stores a password typed while connecting a saved session
// (encrypted, like in the editor) so the next connect needs no prompt.
func (a *App) rememberPassword(s store.Session, pw string) {
	if err := a.store.SetPassword(s.ID, pw); err != nil {
		a.emit(terminal.EventNotify, terminal.Notice{Kind: "err", Message: "Passwort speichern: " + err.Error()})
		return
	}
	a.emit(terminal.EventNotify, terminal.Notice{Kind: "ok", Message: "Passwort für " + s.Name + " verschlüsselt gespeichert."})
	a.emit("sessions:changed", nil)
}
