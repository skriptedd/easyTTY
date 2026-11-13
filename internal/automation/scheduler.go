package automation

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	tickEvery = 10 * time.Second
	// missedGrace: a run that is due more than this late (PC asleep, easyTTY
	// closed) is recorded as missed instead of running late – changes are
	// usually planned for a maintenance window.
	missedGrace = 5 * time.Minute
)

// Target is an opened device connection for one session.
type Target struct {
	Conn       io.ReadWriteCloser
	Name       string
	Transcript io.WriteCloser // optional session log
	LogPath    string
}

// Opener connects to a saved session without any UI prompts.
type Opener func(ctx context.Context, sessionID string) (Target, error)

// ErrBusy is returned when a task is already running.
var ErrBusy = errors.New("Aufgabe läuft bereits")

// Engine runs tasks (on demand and on schedule) and keeps their history.
type Engine struct {
	Store    *Store
	Open     Opener
	OnUpdate func(Run) // optional: progress for the UI
	Now      func() time.Time

	mu      sync.Mutex
	next    map[string]time.Time
	running map[string]context.CancelFunc // task or run id -> cancel
}

// NewEngine creates an engine; call Start to enable the schedule.
func NewEngine(s *Store, open Opener) *Engine {
	return &Engine{Store: s, Open: open, Now: time.Now, next: map[string]time.Time{}, running: map[string]context.CancelFunc{}}
}

func (e *Engine) update(r Run) {
	if e.OnUpdate != nil {
		e.OnUpdate(r)
	}
}

// ExecuteWith runs commands on the sessions one after another through open
// (headless connections or visible tabs) and saves the run. key ("" = run id)
// guards against running the same task twice at once.
func (e *Engine) ExecuteWith(ctx context.Context, key string, sessionIDs []string, commands []string, opts Options, run Run, open Opener) (Run, error) {
	run.ID = NewRunID(e.Now())
	if run.TaskID == "" {
		run.ID = adhocPrefix + run.ID
	} else if taskIDRe.MatchString(run.TaskID) {
		run.ID += "-" + run.TaskID // history per task without reading every file
	}
	if key == "" {
		key = run.ID
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.mu.Lock()
	if e.running[key] != nil {
		e.mu.Unlock()
		return Run{}, ErrBusy
	}
	e.running[key] = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.running, key)
		e.mu.Unlock()
	}()

	run.Start, run.State = e.Now().UnixMilli(), RunRunning
	e.update(run)
	failed := false
	for _, sid := range sessionIDs {
		if ctx.Err() != nil {
			break
		}
		sr := SessionRun{SessionID: sid, Session: sid}
		t, err := open(ctx, sid)
		if err != nil {
			sr.Result.Error = "Verbindung: " + err.Error()
		} else {
			sr.Session, sr.LogPath = t.Name, t.LogPath
			o := opts
			if t.Transcript != nil {
				o.Transcript = t.Transcript
			}
			sr.Result = RunCommands(ctx, t.Conn, commands, o, nil)
			t.Conn.Close()
			if t.Transcript != nil {
				t.Transcript.Close()
			}
		}
		failed = failed || !sr.Result.OK()
		run.Sessions = append(run.Sessions, sr)
		e.update(run.Summary())
	}
	run.End = e.Now().UnixMilli()
	switch {
	case ctx.Err() != nil:
		run.State = RunCancelled
	case failed:
		run.State = RunFailed
	default:
		run.State = RunOK
	}
	err := e.Store.SaveRun(run)
	e.update(run.Summary())
	return run, err
}

// RunTask executes a saved task now.
func (e *Engine) RunTask(ctx context.Context, id, trigger string) (Run, error) {
	return e.RunTaskWith(ctx, id, trigger, e.Open)
}

// RunTaskWith runs a saved task through open.
func (e *Engine) RunTaskWith(ctx context.Context, id, trigger string, open Opener, tweak ...func(*Options)) (Run, error) {
	t, err := e.Store.Task(id)
	if err != nil {
		return Run{}, err
	}
	opts := t.Options()
	for _, f := range tweak {
		f(&opts)
	}
	return e.ExecuteWith(ctx, t.ID, t.SessionIDs, SplitCommands(t.Commands), opts,
		Run{TaskID: t.ID, TaskName: t.Name, Trigger: trigger}, open)
}

// Cancel stops a running task or run.
func (e *Engine) Cancel(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c := e.running[key]; c != nil {
		c()
		return true
	}
	return false
}

// Running lists the keys of running tasks/runs.
func (e *Engine) Running() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, 0, len(e.running))
	for k := range e.running {
		out = append(out, k)
	}
	return out
}

// Next returns the next planned time per enabled task.
func (e *Engine) Next() map[string]int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]int64{}
	for id, t := range e.next {
		out[id] = t.UnixMilli()
	}
	return out
}

// Reschedule recomputes next run times (after tasks changed). An interval
// keeps its pending time so editing other tasks does not postpone it.
func (e *Engine) Reschedule() {
	now := e.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	next := map[string]time.Time{}
	for _, t := range e.Store.Tasks() {
		if !t.Enabled {
			continue
		}
		n, ok := t.Schedule.Next(now)
		prev, had := e.next[t.ID]
		switch {
		case had && !prev.After(now):
			n, ok = prev, true // due but not ticked yet: saving any task must not drop it
		case had && t.Schedule.Kind == ScheduleInterval && !prev.After(n):
			n, ok = prev, true
		}
		if ok {
			next[t.ID] = n
		}
	}
	e.next = next
}

// Start runs the schedule until ctx ends.
func (e *Engine) Start(ctx context.Context) {
	e.recordMissedOnce(e.Now())
	e.Reschedule()
	go func() {
		tk := time.NewTicker(tickEvery)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				e.Tick(ctx)
			}
		}
	}()
}

// Tick starts every task that is due (exported for tests).
func (e *Engine) Tick(ctx context.Context) {
	now := e.Now()
	e.mu.Lock()
	var due []string
	for id, at := range e.next {
		if !at.After(now) {
			due = append(due, id)
		}
	}
	e.mu.Unlock()
	for _, id := range due {
		t, err := e.Store.Task(id)
		e.mu.Lock()
		at, planned := e.next[id]
		if !planned || err != nil || !t.Enabled { // rescheduled, deleted or disabled meanwhile
			delete(e.next, id)
			e.mu.Unlock()
			continue
		}
		if n, ok := t.Schedule.Next(now); ok {
			e.next[id] = n
		} else {
			delete(e.next, id)
		}
		e.mu.Unlock()
		if now.Sub(at) > missedGrace {
			r := Run{ID: NewRunID(now), TaskID: t.ID, TaskName: t.Name, Trigger: "Zeitplan", State: RunMissed, Sessions: []SessionRun{},
				Start: at.UnixMilli(), End: at.UnixMilli(), Error: "PC oder easyTTY war zur geplanten Zeit aus – nicht nachgeholt"}
			_ = e.Store.SaveRun(r)
			e.update(r)
			continue
		}
		go func() {
			if _, err := e.RunTask(ctx, t.ID, "Zeitplan"); errors.Is(err, ErrBusy) {
				r := Run{ID: NewRunID(now), TaskID: t.ID, TaskName: t.Name, Trigger: "Zeitplan", State: RunMissed, Sessions: []SessionRun{},
					Start: now.UnixMilli(), End: now.UnixMilli(), Error: "übersprungen – der vorige Lauf lief noch"}
				_ = e.Store.SaveRun(r)
				e.update(r)
			}
		}()
	}
}

// recordMissedOnce notes one-time tasks whose moment passed while easyTTY was
// closed. Without it they would vanish silently: Reschedule drops them.
func (e *Engine) recordMissedOnce(now time.Time) {
	for _, t := range e.Store.Tasks() {
		if !t.Enabled || t.Schedule.Kind != ScheduleOnce {
			continue
		}
		at, future := t.Schedule.Next(now)
		if future || at.IsZero() || len(e.Store.Runs(t.ID, 1)) > 0 {
			continue
		}
		_ = e.Store.SaveRun(Run{ID: NewRunID(now), TaskID: t.ID, TaskName: t.Name, Trigger: "Zeitplan", State: RunMissed,
			Sessions: []SessionRun{}, Start: at.UnixMilli(), End: at.UnixMilli(), Error: "easyTTY war zur geplanten Zeit geschlossen – nicht nachgeholt"})
	}
}
