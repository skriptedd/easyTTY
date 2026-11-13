package automation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestScheduleNext(t *testing.T) {
	// 2026-11-02 is a Monday.
	cases := []struct {
		s     Schedule
		after string
		want  string // "" = none
	}{
		{Schedule{Kind: ScheduleDaily, At: "02:30"}, "2026-11-02 01:00", "2026-11-02 02:30"},
		{Schedule{Kind: ScheduleDaily, At: "02:30"}, "2026-11-02 02:30", "2026-11-03 02:30"},
		{Schedule{Kind: ScheduleWeekly, At: "22:00", Days: []int{3, 6}}, "2026-11-02 23:00", "2026-11-04 22:00"},
		{Schedule{Kind: ScheduleWeekly, At: "22:00", Days: []int{1}}, "2026-11-02 22:00", "2026-11-09 22:00"},
		{Schedule{Kind: ScheduleWeekly, At: "06:00", Days: []int{7}}, "2026-11-07 12:00", "2026-11-08 06:00"},
		{Schedule{Kind: ScheduleOnce, Date: "2026-11-05", At: "03:00"}, "2026-11-02 10:00", "2026-11-05 03:00"},
		{Schedule{Kind: ScheduleOnce, Date: "2026-11-05", At: "03:00"}, "2026-11-06 10:00", ""},
		{Schedule{Kind: ScheduleInterval, EveryMin: 30}, "2026-11-02 10:07", "2026-11-02 10:37"},
		{Schedule{Kind: ScheduleManual}, "2026-11-02 10:00", ""},
	}
	for _, c := range cases {
		got, ok := c.s.Next(at(c.after))
		if c.want == "" {
			if ok {
				t.Errorf("%+v after %s: unexpected %v", c.s, c.after, got)
			}
			continue
		}
		if !ok || !got.Equal(at(c.want)) {
			t.Errorf("%+v after %s = %v,%v want %s", c.s, c.after, got, ok, c.want)
		}
	}
}

func TestScheduleValidateAndDescribe(t *testing.T) {
	bad := []Schedule{
		{Kind: ScheduleDaily, At: "25:00"},
		{Kind: ScheduleDaily, At: "2:3x"},
		{Kind: ScheduleWeekly, At: "10:00", Days: []int{0, 8}},
		{Kind: ScheduleOnce, Date: "02.11.2026", At: "10:00"},
		{Kind: ScheduleInterval, EveryMin: 1},
		{Kind: "cron"},
	}
	for _, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v sollte ungültig sein", s)
		}
	}
	w := Schedule{Kind: ScheduleWeekly, At: "02:30", Days: []int{5, 1, 3, 1}}
	if err := w.Validate(); err != nil || w.Describe() != "Mo, Mi, Fr um 02:30" {
		t.Errorf("weekly: %v %q", err, w.Describe())
	}
	for _, c := range []struct {
		s    Schedule
		want string
	}{
		{Schedule{Kind: ScheduleDaily, At: "06:00"}, "täglich um 06:00"},
		{Schedule{Kind: ScheduleInterval, EveryMin: 120}, "alle 2 h"},
		{Schedule{Kind: ScheduleInterval, EveryMin: 15}, "alle 15 min"},
		{Schedule{Kind: ScheduleOnce, Date: "2026-11-05", At: "03:00"}, "einmal am 05.11.2026 um 03:00"},
		{Schedule{Kind: ScheduleManual}, "nur von Hand"},
	} {
		if got := c.s.Describe(); got != c.want {
			t.Errorf("Describe = %q want %q", got, c.want)
		}
	}
}

func TestStoreTasksAndRuns(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Task{Name: "x"}); err == nil {
		t.Error("Aufgabe ohne Sessions akzeptiert")
	}
	tk, err := s.Save(Task{Name: " Backup ", SessionIDs: []string{"a"}, Commands: "show run\n", Schedule: Schedule{Kind: ScheduleDaily, At: "02:00"}, CreatedBy: "MCP: test"})
	if err != nil {
		t.Fatal(err)
	}
	if tk.ID == "" || tk.Name != "Backup" || tk.Device != DeviceAuto {
		t.Errorf("task = %+v", tk)
	}
	tk.Commands = "show version"
	tk.CreatedBy = "von außen geändert"
	if tk, err = s.Save(tk); err != nil || tk.CreatedBy != "MCP: test" {
		t.Errorf("update: %v %+v", err, tk)
	}
	s2, err := OpenStore(dir)
	if err != nil || len(s2.Tasks()) != 1 || s2.Tasks()[0].Commands != "show version" {
		t.Fatalf("reload: %v %+v", err, s2.Tasks())
	}
	for i := 0; i < 3; i++ {
		r := Run{ID: NewRunID(time.Now().Add(time.Duration(i) * time.Second)), TaskID: tk.ID, TaskName: "Backup", State: RunOK,
			Sessions: []SessionRun{{Session: "sw1", Result: Result{Steps: []Step{{Command: "show run", Output: "hostname sw1"}}}}}}
		if err := s.SaveRun(r); err != nil {
			t.Fatal(err)
		}
	}
	runs := s.Runs(tk.ID, 0)
	if len(runs) != 3 || runs[0].Sessions[0].Result.Steps != nil || runs[0].ID < runs[2].ID {
		t.Fatalf("runs: %+v", runs)
	}
	full, err := s.Run(runs[0].ID)
	if err != nil || full.Sessions[0].Result.Steps[0].Output != "hostname sw1" {
		t.Errorf("full run: %v %+v", err, full)
	}
	if _, err := s.Run(`..\tasks`); err == nil {
		t.Error("Pfad in Lauf-ID akzeptiert")
	}
	if err := s.Delete(tk.ID); err != nil || len(s.Tasks()) != 0 {
		t.Errorf("delete: %v", err)
	}
}

type fakeOpener struct {
	mu      sync.Mutex
	opened  []string
	devices []*fakeDevice
}

func (f *fakeOpener) open(ctx context.Context, id string) (Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, id)
	if id == "offline" {
		return Target{}, errors.New("Zeitüberschreitung")
	}
	d := newFakeDevice(false)
	f.devices = append(f.devices, d)
	return Target{Conn: closer{d}, Name: "Switch " + id}, nil
}

type closer struct{ *fakeDevice }

func (c closer) Close() error { c.fakeDevice.Close(); return nil }

func TestEngineRunTask(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	op := &fakeOpener{}
	e := NewEngine(s, op.open)
	var updates []string
	var mu sync.Mutex
	e.OnUpdate = func(r Run) { mu.Lock(); updates = append(updates, r.State); mu.Unlock() }
	tk, _ := s.Save(Task{Name: "Doku", SessionIDs: []string{"a", "offline", "b"}, Commands: "show version"})
	r, err := e.RunTask(context.Background(), tk.ID, "von Hand")
	if err != nil {
		t.Fatal(err)
	}
	if r.State != RunFailed || len(r.Sessions) != 3 {
		t.Fatalf("run: %+v", r)
	}
	if !r.Sessions[0].Result.OK() || r.Sessions[0].Session != "Switch a" || !strings.Contains(r.Sessions[1].Result.Error, "Zeitüberschreitung") {
		t.Errorf("sessions: %+v", r.Sessions)
	}
	if !strings.Contains(r.Sessions[2].Result.Steps[0].Output, "Cisco IOS") {
		t.Errorf("b: %+v", r.Sessions[2])
	}
	if len(s.Runs("", 0)) != 1 || updates[0] != RunRunning || updates[len(updates)-1] != RunFailed {
		t.Errorf("history/updates: %v", updates)
	}
}

func TestEngineBusyAndCancel(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	e := NewEngine(s, (&fakeOpener{}).open)
	tk, _ := s.Save(Task{Name: "lang", SessionIDs: []string{"a"}, Commands: "slow\nslow\nslow\nslow"})
	done := make(chan Run)
	go func() { r, _ := e.RunTask(context.Background(), tk.ID, "von Hand"); done <- r }()
	time.Sleep(500 * time.Millisecond)
	if _, err := e.RunTask(context.Background(), tk.ID, "von Hand"); !errors.Is(err, ErrBusy) {
		t.Errorf("zweiter Start: %v", err)
	}
	if len(e.Running()) != 1 || !e.Cancel(tk.ID) {
		t.Error("cancel")
	}
	if r := <-done; r.State != RunCancelled {
		t.Errorf("state %s", r.State)
	}
}

func TestEngineScheduleTick(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	op := &fakeOpener{}
	e := NewEngine(s, op.open)
	now := at("2026-11-02 01:59")
	e.Now = func() time.Time { return now }
	due, _ := s.Save(Task{Name: "nachts", Enabled: true, SessionIDs: []string{"a"}, Commands: "show version", Schedule: Schedule{Kind: ScheduleDaily, At: "02:00"}})
	late, _ := s.Save(Task{Name: "verpasst", Enabled: true, SessionIDs: []string{"b"}, Commands: "show version", Schedule: Schedule{Kind: ScheduleDaily, At: "01:00"}})
	_, _ = s.Save(Task{Name: "aus", Enabled: false, SessionIDs: []string{"c"}, Commands: "x", Schedule: Schedule{Kind: ScheduleDaily, At: "02:00"}})
	e.Reschedule()
	if n := e.Next(); len(n) != 2 || n[due.ID] != at("2026-11-02 02:00").UnixMilli() {
		t.Fatalf("next: %v", n)
	}
	// Pretend the PC slept: "verpasst" was due at 01:00 yesterday → mark missed.
	e.mu.Lock()
	e.next[late.ID] = at("2026-11-01 01:00")
	e.mu.Unlock()
	now = at("2026-11-02 02:00")
	e.Tick(context.Background())
	deadline := time.Now().Add(20 * time.Second)
	for len(s.Runs(due.ID, 0)) == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if runs := s.Runs(due.ID, 0); len(runs) != 1 || runs[0].State != RunOK || runs[0].Trigger != "Zeitplan" {
		t.Fatalf("due run: %+v", runs)
	}
	if runs := s.Runs(late.ID, 0); len(runs) != 1 || runs[0].State != RunMissed {
		t.Fatalf("missed run: %+v", runs)
	}
	if n := e.Next(); n[due.ID] != at("2026-11-03 02:00").UnixMilli() {
		t.Errorf("rescheduled: %v", time.UnixMilli(n[due.ID]))
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if len(op.opened) != 1 || op.opened[0] != "a" {
		t.Errorf("opened %v", op.opened)
	}
}

func TestRescheduleKeepsInterval(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	e := NewEngine(s, (&fakeOpener{}).open)
	now := at("2026-11-02 10:00")
	e.Now = func() time.Time { return now }
	tk, _ := s.Save(Task{Name: "poll", Enabled: true, SessionIDs: []string{"a"}, Commands: "x", Schedule: Schedule{Kind: ScheduleInterval, EveryMin: 30}})
	e.Reschedule()
	now = at("2026-11-02 10:20")
	e.Reschedule()
	if got := e.Next()[tk.ID]; got != at("2026-11-02 10:30").UnixMilli() {
		t.Errorf("interval verschoben: %v", time.UnixMilli(got))
	}
}

func TestHiddenCharsRejected(t *testing.T) {
	for _, cmds := range []string{"show version\rreload", "show clock\x03", "show ‮loader", "show\x1b[2Jx"} {
		task := Task{Name: "x", SessionIDs: []string{"a"}, Commands: cmds}
		if err := task.Validate(); err == nil {
			t.Errorf("%q accepted", cmds)
		}
	}
	task := Task{Name: "Bad\x1bName", SessionIDs: []string{"a"}, Commands: "show clock"}
	if err := task.Validate(); err == nil {
		t.Error("name with ESC accepted")
	}
	if res := RunCommands(context.Background(), nil, []string{"show clock\rreload"}, Options{}, nil); res.Error == "" {
		t.Error("runner must refuse hidden characters before writing")
	}
	if got := CleanLabel("Claude\nWann: nie‮", 60); got != "Claude Wann: nie" {
		t.Errorf("CleanLabel = %q", got)
	}
}

func TestRunIDsAndAdhocQuota(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	for _, bad := range []string{"CON", "nul", `..\x`, "20261102-071552-3046be.json"} {
		if _, err := s.Run(bad); err == nil {
			t.Errorf("run id %q accepted", bad)
		}
	}
	base := time.Date(2026, 11, 2, 1, 0, 0, 0, time.Local)
	task := Run{ID: NewRunID(base), TaskID: "k", State: RunOK}
	if err := s.SaveRun(task); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < keepRuns+5; i++ {
		if err := s.SaveRun(Run{ID: adhocPrefix + NewRunID(base.Add(time.Duration(i+1)*time.Second)), State: RunOK}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Run(task.ID); err != nil {
		t.Errorf("task run evicted by ad-hoc runs: %v", err)
	}
	all := s.Runs("", 1000)
	if len(all) != keepRuns+1 || !strings.HasPrefix(all[0].ID, adhocPrefix) || all[len(all)-1].ID != task.ID {
		t.Errorf("runs: %d first %s last %s", len(all), all[0].ID, all[len(all)-1].ID)
	}
}

func TestRescheduleKeepsDueRun(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	e := NewEngine(s, (&fakeOpener{}).open)
	now := at("2026-11-02 09:59")
	e.Now = func() time.Time { return now }
	tk, _ := s.Save(Task{Name: "zehn", Enabled: true, SessionIDs: []string{"a"}, Commands: "x", Schedule: Schedule{Kind: ScheduleDaily, At: "10:00"}})
	e.Reschedule()
	now = at("2026-11-02 10:00").Add(2 * time.Second) // due, tick not yet run
	_, _ = s.Save(Task{Name: "andere", SessionIDs: []string{"b"}, Commands: "y"})
	e.Reschedule()
	if got := e.Next()[tk.ID]; got != at("2026-11-02 10:00").UnixMilli() {
		t.Errorf("due run lost on reschedule: next=%v", time.UnixMilli(got))
	}
}

func TestRunHistoryPerTask(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	base := time.Date(2026, 11, 2, 1, 0, 0, 0, time.Local)
	busy, quiet := "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"
	if err := s.SaveRun(Run{ID: NewRunID(base) + "-" + quiet, TaskID: quiet, State: RunOK}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < keepPerTask+20; i++ {
		id := NewRunID(base.Add(time.Duration(i+1)*time.Minute)) + "-" + busy
		if err := s.SaveRun(Run{ID: id, TaskID: busy, State: RunOK}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(s.Runs(busy, 1000)); n != keepPerTask {
		t.Errorf("busy task keeps %d runs, want %d", n, keepPerTask)
	}
	if r := s.Runs(quiet, 10); len(r) != 1 {
		t.Errorf("quiet task lost its history: %d", len(r))
	}
}

func TestCorruptTasksFileFallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := Task{Name: "a", SessionIDs: []string{"s"}, Commands: "show clock", Schedule: Schedule{Kind: ScheduleManual}}
	first, err := s.Save(base)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Name = "b"
	if _, err := s.Save(second); err != nil { // the first state is now in .bak
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte("{kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("backup not used: %v", err)
	}
	if tasks := again.Tasks(); len(tasks) != 1 || tasks[0].Name != "a" {
		t.Fatalf("restored: %+v", tasks)
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks.json.damaged")); err != nil {
		t.Errorf("damaged file not kept: %v", err)
	}
}

func TestFailedSaveKeepsMemoryAndDiskInStep(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "datei")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Store{dir: filepath.Join(blocker, "unterordner")} // cannot be created
	_, err := s.Save(Task{Name: "a", SessionIDs: []string{"s"}, Commands: "show clock", Schedule: Schedule{Kind: ScheduleManual}})
	if err == nil {
		t.Fatal("save must fail")
	}
	if len(s.Tasks()) != 0 {
		t.Fatal("task stayed in memory although it was never written")
	}
}

func TestMissedOneTimeTaskIsRecordedOnce(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	task, err := s.Save(Task{Name: "fenster", Enabled: true, SessionIDs: []string{"s"}, Commands: "reload",
		Schedule: Schedule{Kind: ScheduleOnce, Date: "2026-11-04", At: "02:00"}})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Store: s, Now: func() time.Time { return now }}
	e.recordMissedOnce(now)
	e.recordMissedOnce(now)
	runs := s.Runs(task.ID, 10)
	if len(runs) != 1 || runs[0].State != RunMissed {
		t.Fatalf("runs: %+v", runs)
	}
}

func TestCapOutputKeepsTheEnd(t *testing.T) {
	big := append(bytes.Repeat([]byte("x"), maxOutputBytes), []byte("sw1#")...)
	got := capOutput(big)
	if len(got) > maxOutputBytes || !bytes.HasSuffix(got, []byte("sw1#")) || !bytes.Contains(got, []byte("gekürzt")) {
		t.Fatalf("len %d, tail %q", len(got), got[len(got)-8:])
	}
	if small := []byte("ok"); !bytes.Equal(capOutput(small), small) {
		t.Fatal("small output must stay")
	}
}
