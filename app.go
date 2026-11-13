package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"easytty/internal/approval"
	"easytty/internal/automation"
	"easytty/internal/conn"
	"easytty/internal/fsutil"
	"easytty/internal/fwserver"
	"easytty/internal/mcpserver"
	"easytty/internal/rdpview"
	"easytty/internal/store"
	"easytty/internal/terminal"
)

const (
	appName       = "easyTTY"
	promptTimeout = 5 * time.Minute
)

// appVersion is set by release builds: -ldflags "-X main.appVersion=1.0.0".
var appVersion = "0.0.0-dev"

// App is the Wails-bound backend. Exported methods are callable from the UI.
type App struct {
	ctx          context.Context
	dir          string
	store        *store.Store
	settingsPath string
	terms        *terminal.Manager
	knownHosts   *conn.KnownHosts
	home         string
	portable     bool // data lives next to the exe (see portable.go)

	mu         sync.Mutex
	dials      map[string]*dial              // tabId -> pending dial
	sftp       map[string]*sftp.Client       // tabId -> lazily opened SFTP
	jobs       map[string]context.CancelFunc // jobId -> running transfer
	pastes     map[string]context.CancelFunc // tabId -> running paced paste
	statusKind map[string]string             // tabId -> detected device kind (status panel)
	prompts    map[string]chan promptAnswer
	lastDown   string
	fw         *fwserver.Server // firmware server, created on first use
	fwEvents   []fwserver.Event
	auto       *automation.Engine // scheduled tasks (nil if the store failed to load)
	rdp        *rdpview.Host      // RDP controls inside the window, started on first use
	relaunch   string             // exe to start after shutdown (set by an update)
	settingsMu sync.Mutex         // one read-modify-write of settings.json at a time
	updating   atomic.Bool

	// MCP (AI access): endpoint, open tabs it may see, approval gate.
	gate     *approval.Gate
	mcp      *mcpserver.Listener
	mcpPort  int
	mcpToken string
	mcpErr   error
	tabMeta  map[string]tabMeta
	mcpRuns  chan struct{}     // limits concurrent run_commands
	aiTabs   map[string]string // tabId -> run key while automation types into it
}

type promptAnswer struct {
	ok     bool
	values []string
}

// PromptEvent is sent to the UI when the backend needs user input.
type PromptEvent struct {
	ID      string              `json:"id"`
	TabID   string              `json:"tabId"`
	Kind    string              `json:"kind"`
	Title   string              `json:"title"`
	Message string              `json:"message"`
	Fields  []conn.PromptField  `json:"fields"`
	HostKey *conn.HostKeyPrompt `json:"hostKey,omitempty"`
}

// NewApp creates the backend; heavy initialisation happens in startup.
func NewApp() *App {
	a := &App{
		dials:      map[string]*dial{},
		sftp:       map[string]*sftp.Client{},
		jobs:       map[string]context.CancelFunc{},
		pastes:     map[string]context.CancelFunc{},
		statusKind: map[string]string{},
		prompts:    map[string]chan promptAnswer{},
		tabMeta:    map[string]tabMeta{},
		mcpRuns:    make(chan struct{}, maxMcpConcurrent),
		aiTabs:     map[string]string{},
	}
	a.gate = approval.NewGate(approvalUI{a}, fsutil.NewID)
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.terms = terminal.NewManager(a.emit)
	a.home, _ = os.UserHomeDir()

	var err error
	if a.dir, a.portable, err = dataDir(); err != nil {
		a.fatal(err)
		return
	}
	a.settingsPath = filepath.Join(a.dir, "settings.json")
	a.store, err = store.Open(filepath.Join(a.dir, "sessions.json"), store.DefaultProtector())
	if err != nil {
		a.fatal(err)
		return
	}
	a.knownHosts = conn.NewKnownHosts(
		filepath.Join(a.dir, "known_hosts"),
		[]string{filepath.Join(a.home, ".ssh", "known_hosts")},
		nil, // confirm is bound per dial, see hostKeyConfirm
	)
	a.lastDown = filepath.Join(a.home, "Downloads")
	a.startAutomation()
	a.applyMCP()
	a.startUpdateCheck()
	// File drops from Explorer are registered in the UI (window.runtime.OnFileDrop),
	// which also installs Wails' drop listeners; a Go-side handler alone never fires.
}

func (a *App) fatal(err error) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type: runtime.ErrorDialog, Title: appName, Message: "Start fehlgeschlagen:\n" + err.Error(),
	})
	runtime.Quit(a.ctx)
}

func (a *App) beforeClose(ctx context.Context) bool {
	open := 0
	if a.terms != nil {
		open = a.terms.Count()
	}
	if h := a.runningRDP(); h != nil {
		open += h.Count()
	}
	if open == 0 || a.restarting() {
		return false
	}
	s, _ := store.LoadSettings(a.settingsPath)
	if !s.ConfirmClose {
		return false
	}
	res, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         appName,
		Message:       fmt.Sprintf("%d Verbindung(en) offen. Wirklich beenden?", open),
		Buttons:       []string{"Yes", "No"},
		DefaultButton: "No",
	})
	return err == nil && res != "Yes" && res != "Ja"
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	for _, d := range a.dials {
		d.cancel()
	}
	for _, cancel := range a.jobs {
		cancel()
	}
	fw := a.fw
	a.mu.Unlock()
	if fw != nil {
		fw.Stop()
	}
	a.stopMCP()
	if h := a.runningRDP(); h != nil {
		h.Shutdown()
	}
	if a.terms != nil {
		a.terms.CloseAll()
	}
	a.relaunchIfWanted()
}

func (a *App) quit() { runtime.Quit(a.ctx) }

func (a *App) emit(event string, payload any) {
	runtime.EventsEmit(a.ctx, event, payload)
}

// ask shows a prompt in the UI and blocks until it is answered, cancelled or times out.
func (a *App) ask(ctx context.Context, ev PromptEvent) promptAnswer {
	ev.ID = fsutil.NewID()
	ch := make(chan promptAnswer, 1)
	a.mu.Lock()
	a.prompts[ev.ID] = ch
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.prompts, ev.ID)
		a.mu.Unlock()
	}()
	a.emit("prompt", ev)
	select {
	case ans := <-ch:
		return ans
	case <-ctx.Done():
	case <-time.After(promptTimeout):
	}
	a.emit("prompt:cancel", ev.ID)
	return promptAnswer{}
}

// AnswerPrompt is called by the UI to resolve a pending prompt.
func (a *App) AnswerPrompt(id string, ok bool, values []string) {
	a.mu.Lock()
	ch := a.prompts[id]
	a.mu.Unlock()
	if ch != nil {
		select {
		case ch <- promptAnswer{ok: ok, values: values}:
		default:
		}
	}
}

// hostKeyConfirm returns a confirm function bound to one tab's dial.
// target is the host the tab connects to: a key of any other host on the way
// belongs to a jump host and is named as such.
func (a *App) hostKeyConfirm(ctx context.Context, tabID, target string) conn.HostKeyConfirm {
	return func(p conn.HostKeyPrompt) bool {
		title := "Unbekannter Host"
		msg := "Der Host " + p.Host + " ist noch nicht bekannt. Fingerprint prüfen und vertrauen?"
		if p.Changed {
			title = "WARNUNG: Hostkey hat sich geändert"
			msg = "Der Schlüssel von " + p.Host + " ist ANDERS als gespeichert. Das kann ein Man-in-the-Middle-Angriff sein – oder das Gerät wurde neu aufgesetzt."
		}
		if isJumpHost(p.Host, target) {
			title = "Sprung-Host: " + title
			msg = "Das ist der Sprung-Host auf dem Weg zu " + target + ". " + msg
		}
		ans := a.ask(ctx, PromptEvent{TabID: tabID, Kind: "hostkey", Title: title, Message: msg, HostKey: &p})
		return ans.ok
	}
}

// AppInfo returns name, version and data directory.
func (a *App) AppInfo() map[string]string {
	mode := "installiert"
	if a.portable {
		mode = "portabel"
	}
	return map[string]string{"name": appName, "version": appVersion, "dataDir": a.dir, "mode": mode}
}

// OpenDataDir opens the config directory in Explorer.
func (a *App) OpenDataDir() error {
	return exec.Command(explorerExe(), a.dir).Start()
}

// systemRoot is the Windows folder; programs are started by full path so a
// changed PATH cannot swap them.
func systemRoot() string {
	if r := os.Getenv("SystemRoot"); r != "" {
		return r
	}
	return `C:\Windows`
}

func explorerExe() string { return filepath.Join(systemRoot(), "explorer.exe") }
