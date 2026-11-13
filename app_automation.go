package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"easytty/internal/automation"
	"easytty/internal/conn"
	"easytty/internal/store"
	"easytty/internal/terminal"
)

const (
	headlessCols    = 250 // wide, so devices do not wrap long lines
	headlessRows    = 50
	headlessTimeout = 20 * time.Second
)

// EventTaskRun carries automation.Run updates to the UI.
const EventTaskRun = "task:run"

// startAutomation opens the task store and starts the schedule.
func (a *App) startAutomation() {
	st, err := automation.OpenStore(filepath.Join(a.dir, "automation"))
	if err != nil {
		a.emit(terminal.EventNotify, terminal.Notice{Kind: "err", Message: "Aufgaben nicht geladen: " + err.Error()})
		return
	}
	e := automation.NewEngine(st, a.openHeadless)
	e.OnUpdate = func(r automation.Run) { a.emit(EventTaskRun, r) }
	e.Start(a.ctx)
	a.mu.Lock()
	a.auto = e
	a.mu.Unlock()
}

func (a *App) engine() (*automation.Engine, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.auto == nil {
		return nil, errors.New("Aufgaben sind nicht verfügbar (siehe Meldung beim Start)")
	}
	return a.auto, nil
}

// openHeadless connects to a saved session without any dialog: unknown host
// keys and missing credentials fail with a clear message instead of a prompt
// nobody answers at 2 a.m.
func (a *App) openHeadless(ctx context.Context, sessionID string) (automation.Target, error) {
	s, err := a.store.Session(sessionID)
	if err != nil {
		return automation.Target{}, err
	}
	var c terminal.Conn
	switch s.Protocol {
	case store.ProtocolSerial:
		sc, err := conn.OpenSerial(conn.SerialConfig{Port: s.SerialPort, Baud: s.Baud, DataBits: s.DataBits, Parity: s.Parity, StopBits: s.StopBits})
		if err != nil {
			return automation.Target{}, fmt.Errorf("%s nicht zu öffnen (in einem Tab offen?): %w", s.SerialPort, err)
		}
		c = sc
	case store.ProtocolSSH:
		pw, _ := a.store.Password(s.ID)
		d := conn.Dialer{
			HostKeys:          a.knownHosts.CallbackWith(func(conn.HostKeyPrompt) bool { return false }),
			HostKeyAlgorithms: a.knownHosts.HostKeyAlgorithms,
			AgentDial:         conn.DialAgent,
			HomeDir:           a.home,
			Ask:               func(conn.PromptRequest) ([]string, bool) { return nil, false },
			HandshakeTimeout:  headlessTimeout,
		}
		ctx, cancel := context.WithTimeout(ctx, 2*headlessTimeout)
		defer cancel()
		cfg, err := withJumps(conn.SSHConfig{Host: s.Host, Port: s.Port, User: s.User, Auth: s.Auth, KeyPath: s.KeyPath,
			Password: pw, KeepAlive: 30 * time.Second, Cols: headlessCols, Rows: headlessRows}, s, a.lookupSession)
		if err != nil {
			return automation.Target{}, err
		}
		sc, err := d.Dial(ctx, cfg)
		if err != nil {
			return automation.Target{}, explainHeadless(s, err)
		}
		c = sc
	case store.ProtocolRDP:
		return automation.Target{}, fmt.Errorf("%s ist eine RDP-Session – Befehle gehen nur über SSH oder Seriell", s.Name)
	default:
		return automation.Target{}, errors.New("unbekanntes Protokoll")
	}
	pw, _ := a.store.Password(s.ID)
	conn, err := loginIfNeeded(ctx, c, s, pw)
	if err != nil {
		conn.Close()
		return automation.Target{}, err
	}
	t := automation.Target{Conn: conn, Name: s.Name}
	if l, path := a.openLog(s); l != nil {
		t.Transcript, t.LogPath = l, path
	}
	return t, nil
}

// explainHeadless turns dial errors into what the user has to fix.
func explainHeadless(s store.Session, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(strings.ToLower(msg), "host") && strings.Contains(strings.ToLower(msg), "key"):
		return fmt.Errorf("Hostkey von %s ist unbekannt oder hat sich geändert – einmal von Hand verbinden und bestätigen (%s)", s.Host, msg)
	case strings.Contains(msg, "abgebrochen") || s.User == "":
		return fmt.Errorf("Anmeldung an %s ohne Rückfrage nicht möglich – Benutzer und Passwort/Schlüssel in der Session speichern (%s)", s.Host, msg)
	}
	return err
}

// TaskView is one row of the task list.
type TaskView struct {
	Task     automation.Task `json:"task"`
	When     string          `json:"when"` // schedule in words
	Next     int64           `json:"next"` // unix ms, 0 = none
	Running  bool            `json:"running"`
	LastRun  *automation.Run `json:"lastRun,omitempty"`
	Sessions []string        `json:"sessions"` // names
}

// TaskOverview lists all tasks with next run, state and last result.
func (a *App) TaskOverview() ([]TaskView, error) {
	e, err := a.engine()
	if err != nil {
		return nil, err
	}
	next, running := e.Next(), e.Running()
	names := map[string]string{}
	for _, s := range a.store.Tree().Sessions {
		names[s.ID] = s.Name
	}
	tasks := e.Store.Tasks()
	out := make([]TaskView, 0, len(tasks))
	for _, t := range tasks {
		v := TaskView{Task: t, When: t.Schedule.Describe(), Next: next[t.ID], Running: slices.Contains(running, t.ID)}
		if runs := e.Store.Runs(t.ID, 1); len(runs) > 0 {
			v.LastRun = &runs[0]
		}
		for _, id := range t.SessionIDs {
			if n, ok := names[id]; ok {
				v.Sessions = append(v.Sessions, n)
			} else {
				v.Sessions = append(v.Sessions, "(gelöschte Session)")
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// SaveTask creates or updates a task and reschedules.
func (a *App) SaveTask(t automation.Task) (automation.Task, error) {
	e, err := a.engine()
	if err != nil {
		return t, err
	}
	saved, err := e.Store.Save(t)
	if err == nil {
		e.Reschedule()
	}
	return saved, err
}

// DeleteTask removes a task (its history stays).
func (a *App) DeleteTask(id string) error {
	e, err := a.engine()
	if err != nil {
		return err
	}
	if err := e.Store.Delete(id); err != nil {
		return err
	}
	e.Cancel(id)
	e.Reschedule()
	return nil
}

// RunTaskNow starts a task in the background; progress arrives as task:run.
func (a *App) RunTaskNow(id string) error {
	e, err := a.engine()
	if err != nil {
		return err
	}
	if slices.Contains(e.Running(), id) {
		return automation.ErrBusy
	}
	t, err := e.Store.Task(id)
	if err != nil {
		return err
	}
	go func() {
		// By hand = the user watches: typed into visible tabs, readable speed.
		open := a.visibleOpener("Aufgabe „"+t.Name+"“", id)
		if _, err := e.RunTaskWith(a.ctx, id, "von Hand", open, a.readable); err != nil && !errors.Is(err, automation.ErrBusy) {
			a.emit(terminal.EventNotify, terminal.Notice{Kind: "err", Message: "Aufgabe: " + err.Error()})
		}
	}()
	return nil
}

// CancelTask stops a running task or run.
func (a *App) CancelTask(id string) bool {
	e, err := a.engine()
	return err == nil && e.Cancel(id)
}

// TaskRuns lists run summaries (taskID "" = all), newest first.
func (a *App) TaskRuns(taskID string, limit int) ([]automation.Run, error) {
	e, err := a.engine()
	if err != nil {
		return nil, err
	}
	runs := e.Store.Runs(taskID, limit)
	if runs == nil {
		runs = []automation.Run{}
	}
	return runs, nil
}

// TaskRun returns one run with full output.
func (a *App) TaskRun(id string) (automation.Run, error) {
	e, err := a.engine()
	if err != nil {
		return automation.Run{}, err
	}
	return e.Store.Run(id)
}
