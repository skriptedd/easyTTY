package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"easytty/internal/fsutil"
)

// Device and option limits for a task.
const (
	maxTaskCommands = 2000
	maxIdleTimeout  = 600 // s
	keepRuns        = 200
)

// Task is a saved command list for one or more sessions.
type Task struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	SessionIDs  []string `json:"sessionIds"`
	Commands    string   `json:"commands"`
	Schedule    Schedule `json:"schedule"`
	Enabled     bool     `json:"enabled"`
	Device      string   `json:"device"` // auto | cisco | other
	StopOnError bool     `json:"stopOnError"`
	AutoConfirm bool     `json:"autoConfirm"`
	IdleTimeout int      `json:"idleTimeout"`          // s, 0 = 30
	CreatedBy   string   `json:"createdBy,omitempty"`  // "" = by hand, otherwise e.g. "MCP: Claude"
	ModifiedBy  string   `json:"modifiedBy,omitempty"` // who changed it last ("" = by hand)
	Created     int64    `json:"created"`
	Updated     int64    `json:"updated"`
}

// Validate checks and normalises a task.
func (t *Task) Validate() error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return errors.New("Name fehlt")
	}
	if HasHiddenChars(t.Name) {
		return errors.New("Name enthält Steuerzeichen")
	}
	if len(t.SessionIDs) == 0 {
		return errors.New("mindestens eine Session wählen")
	}
	cmds := SplitCommands(t.Commands)
	if err := CheckCommands(cmds); err != nil {
		return err
	}
	n := len(cmds)
	if n == 0 {
		return errors.New("keine Befehle")
	}
	if n > maxTaskCommands {
		return fmt.Errorf("höchstens %d Befehle", maxTaskCommands)
	}
	switch t.Device {
	case "":
		t.Device = DeviceAuto
	case DeviceAuto, DeviceCisco, DeviceOther:
	default:
		return fmt.Errorf("unbekannter Gerätetyp %q", t.Device)
	}
	if t.IdleTimeout < 0 || t.IdleTimeout > maxIdleTimeout {
		return fmt.Errorf("Wartezeit zwischen 0 und %d s", maxIdleTimeout)
	}
	if t.Schedule.Kind == "" {
		t.Schedule.Kind = ScheduleManual
	}
	return t.Schedule.Validate()
}

// Options returns the runner options for this task.
func (t Task) Options() Options {
	return Options{Device: t.Device, StopOnError: t.StopOnError, AutoConfirm: t.AutoConfirm,
		IdleTimeout: time.Duration(t.IdleTimeout) * time.Second}
}

// Run states.
const (
	RunRunning   = "läuft"
	RunOK        = "ok"
	RunFailed    = "fehler"
	RunCancelled = "abgebrochen"
	RunMissed    = "verpasst"
)

// SessionRun is the outcome on one device.
type SessionRun struct {
	SessionID string `json:"sessionId"`
	Session   string `json:"session"`
	Result    Result `json:"result"`
	LogPath   string `json:"logPath,omitempty"`
}

// Run is one execution of a task (or of ad-hoc commands from MCP).
type Run struct {
	ID       string       `json:"id"`
	TaskID   string       `json:"taskId,omitempty"`
	TaskName string       `json:"taskName"`
	Trigger  string       `json:"trigger"` // Zeitplan | von Hand | MCP: …
	Start    int64        `json:"start"`
	End      int64        `json:"end"`
	State    string       `json:"state"`
	Error    string       `json:"error,omitempty"`
	Sessions []SessionRun `json:"sessions"`
}

// Summary returns the run without command output (for lists).
func (r Run) Summary() Run {
	out := r
	out.Sessions = make([]SessionRun, len(r.Sessions))
	for i, s := range r.Sessions {
		s.Result.Steps = nil
		out.Sessions[i] = s
	}
	return out
}

// Store keeps tasks in tasks.json and runs in runs\<id>.json.
type Store struct {
	mu    sync.Mutex
	dir   string
	tasks []Task
}

// OpenStore loads tasks from dir (missing file = no tasks).
func OpenStore(dir string) (*Store, error) {
	s := &Store{dir: dir}
	raw, err := os.ReadFile(s.tasksFile())
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.tasks); err != nil {
		s.tasks = nil
		bak, berr := os.ReadFile(s.tasksFile() + ".bak")
		if berr != nil || json.Unmarshal(bak, &s.tasks) != nil {
			return nil, fmt.Errorf("tasks.json beschädigt: %w", err)
		}
		_ = os.WriteFile(s.tasksFile()+".damaged", raw, 0o600) // kept for inspection
	}
	return s, nil
}

func (s *Store) tasksFile() string { return filepath.Join(s.dir, "tasks.json") }
func (s *Store) runsDir() string   { return filepath.Join(s.dir, "runs") }

// commitLocked writes next and only then makes it current, so memory and disk
// never disagree after a failed write.
func (s *Store) commitLocked(next []Task) error {
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(s.tasksFile(), raw); err != nil {
		return err
	}
	s.tasks = next
	return nil
}

// Tasks returns all tasks sorted by name.
func (s *Store) Tasks() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Task(nil), s.tasks...)
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Task returns one task.
func (s *Store) Task(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return Task{}, fmt.Errorf("Aufgabe %q nicht gefunden", id)
}

// Save creates (empty ID) or updates a task.
func (s *Store) Save(t Task) (Task, error) {
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	t.Updated = now
	var next []Task
	if t.ID == "" {
		t.ID, t.Created = fsutil.NewID(), now
		next = append(append([]Task(nil), s.tasks...), t)
	} else {
		i := s.indexLocked(t.ID)
		if i < 0 {
			return Task{}, fmt.Errorf("Aufgabe %q nicht gefunden", t.ID)
		}
		t.Created, t.CreatedBy = s.tasks[i].Created, s.tasks[i].CreatedBy
		next = append(append(append([]Task(nil), s.tasks[:i]...), t), s.tasks[i+1:]...)
	}
	return t, s.commitLocked(next)
}

// Delete removes a task (its run history stays).
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return fmt.Errorf("Aufgabe %q nicht gefunden", id)
	}
	return s.commitLocked(append(append([]Task(nil), s.tasks[:i]...), s.tasks[i+1:]...))
}

func (s *Store) indexLocked(id string) int {
	for i, t := range s.tasks {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// adhocPrefix marks runs without a task (MCP run_commands); they are pruned
// separately so a flood of them never evicts the history of real tasks.
const adhocPrefix = "a-"

var (
	runIDRe  = regexp.MustCompile(`^(a-)?[0-9]{8}-[0-9]{6}-[0-9a-f]{6}(-[0-9a-f]{16})?$`)
	taskIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// keepPerTask runs are kept for each task; ad-hoc and old runs share keepRuns.
const keepPerTask = 50

// runGroup is the pruning group of a run file: "a" (ad-hoc), a task id, or "" (old files).
func runGroup(name string) string {
	id := strings.TrimSuffix(name, ".json")
	if strings.HasPrefix(id, adhocPrefix) {
		return "a"
	}
	if parts := strings.Split(id, "-"); len(parts) == 4 {
		return parts[3]
	}
	return ""
}

// runKey sorts runs by time regardless of the ad-hoc prefix.
func runKey(name string) string {
	return strings.TrimPrefix(strings.TrimSuffix(name, ".json"), adhocPrefix)
}

// SaveRun writes a run and prunes its group (task runs / ad-hoc runs) to keepRuns.
func (s *Store) SaveRun(r Run) error {
	if !runIDRe.MatchString(r.ID) {
		return errors.New("ungültige Lauf-ID")
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(filepath.Join(s.runsDir(), r.ID+".json"), raw); err != nil {
		return err
	}
	ents, err := os.ReadDir(s.runsDir())
	if err != nil {
		return nil
	}
	group := runGroup(r.ID)
	keep := keepRuns
	if group != "a" && group != "" {
		keep = keepPerTask
	}
	var same []string
	for _, e := range ents {
		if runGroup(e.Name()) == group {
			same = append(same, e.Name())
		}
	}
	if len(same) <= keep {
		return nil
	}
	sort.Slice(same, func(i, j int) bool { return runKey(same[i]) < runKey(same[j]) })
	for _, name := range same[:len(same)-keep] {
		_ = os.Remove(filepath.Join(s.runsDir(), name))
	}
	return nil
}

// Run loads one run with full output.
func (s *Store) Run(id string) (Run, error) {
	if !runIDRe.MatchString(id) {
		return Run{}, errors.New("ungültige Lauf-ID")
	}
	var r Run
	raw, err := os.ReadFile(filepath.Join(s.runsDir(), id+".json"))
	if err != nil {
		return r, fmt.Errorf("Lauf %q nicht gefunden", id)
	}
	return r, json.Unmarshal(raw, &r)
}

// Runs lists run summaries, newest first; taskID "" = all; limit 0 = 50.
func (s *Store) Runs(taskID string, limit int) []Run {
	if limit <= 0 {
		limit = 50
	}
	ents, _ := os.ReadDir(s.runsDir())
	sort.Slice(ents, func(i, j int) bool { return runKey(ents[i].Name()) > runKey(ents[j].Name()) })
	var out []Run
	for _, e := range ents {
		if len(out) >= limit {
			break
		}
		if g := runGroup(e.Name()); taskID != "" && g != "" && g != taskID {
			continue // another task's run: skip without reading the file
		}
		r, err := s.Run(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil || (taskID != "" && r.TaskID != taskID) {
			continue
		}
		out = append(out, r.Summary())
	}
	return out
}

// NewRunID sorts by time ("20261102-023000-ab12cd").
func NewRunID(now time.Time) string {
	return now.Format("20060102-150405") + "-" + fsutil.NewID()[:6]
}
