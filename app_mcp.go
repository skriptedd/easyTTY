package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"easytty/internal/approval"
	"easytty/internal/automation"
	"easytty/internal/fsutil"
	"easytty/internal/mcpserver"
	"easytty/internal/store"
	"easytty/internal/terminal"
)

const (
	maxReadLines     = 2000
	maxMcpSessions   = 20  // devices per run_commands
	maxMcpCommands   = 200 // commands per run_commands
	maxMcpConcurrent = 2   // run_commands executing at once
)

// tabMeta is what MCP may know about an open tab.
type tabMeta struct {
	title     string
	sessionID string
}

// TrackTab registers a tab title for MCP (called by the UI when a tab opens).
func (a *App) TrackTab(tabID, title, sessionID string) {
	a.mu.Lock()
	a.tabMeta[tabID] = tabMeta{title: title, sessionID: sessionID}
	a.mu.Unlock()
}

/* ---------- approval ---------- */

// Events for the approval dialog.
const (
	EventApprove       = "mcp:approve"
	EventApproveCancel = "mcp:approve:cancel"
	EventApproveDone   = "mcp:approve:done"
)

// approvalUI shows the questions of the approval gate in the window.
type approvalUI struct{ a *App }

func (u approvalUI) Ask(e approval.Event) {
	runtime.WindowUnminimise(u.a.ctx) // a request must not wait unseen behind other windows
	runtime.WindowShow(u.a.ctx)
	u.a.emit(EventApprove, e)
}
func (u approvalUI) Done(id string)      { u.a.emit(EventApproveDone, id) }
func (u approvalUI) Cancelled(id string) { u.a.emit(EventApproveCancel, id) }

// AnswerApproval is called by the approval dialog: once | grant | deny.
func (a *App) AnswerApproval(id, answer string) { a.gate.Answer(id, answer) }

/* ---------- mcpserver.Backend ---------- */

type mcpBackend struct{ a *App }

func (b mcpBackend) folderPath(tree store.Tree, id string) string {
	byID := map[string]store.Folder{}
	for _, f := range tree.Folders {
		byID[f.ID] = f
	}
	var parts []string
	for f, ok := byID[id]; ok && len(parts) < 32; f, ok = byID[f.ParentID] {
		parts = append([]string{f.Name}, parts...)
	}
	return strings.Join(parts, " / ")
}

func target(s store.Session) string {
	if s.Protocol == store.ProtocolSerial {
		return s.SerialPort
	}
	return fmt.Sprintf("%s@%s:%d", s.User, s.Host, s.Port)
}

func (b mcpBackend) Sessions() []mcpserver.SessionInfo {
	tree := b.a.store.Tree()
	out := make([]mcpserver.SessionInfo, 0, len(tree.Sessions))
	for _, s := range tree.Sessions {
		if s.Protocol == store.ProtocolRDP {
			continue // no terminal, nothing an AI could run there
		}
		info := mcpserver.SessionInfo{ID: s.ID, Name: s.Name, Folder: b.folderPath(tree, s.FolderID), Protocol: s.Protocol, Target: target(s)}
		if s.Protocol == store.ProtocolSerial {
			info.HasCredentials = true
		} else {
			info.HasCredentials = s.User != "" && (s.HasPassword || s.Auth != store.AuthPassword)
		}
		out = append(out, info)
	}
	return out
}

func (b mcpBackend) Tabs() []mcpserver.TabInfo {
	tree := b.a.store.Tree()
	folderOf := map[string]string{}
	for _, s := range tree.Sessions {
		folderOf[s.ID] = b.folderPath(tree, s.FolderID)
	}
	b.a.mu.Lock()
	defer b.a.mu.Unlock()
	var out []mcpserver.TabInfo
	for id, m := range b.a.tabMeta {
		if _, err := b.a.terms.Conn(id); err != nil {
			continue // not connected (any more)
		}
		out = append(out, mcpserver.TabInfo{TabID: id, Title: automation.CleanLabel(m.title, maxTabTitle), SessionID: m.sessionID, Folder: folderOf[m.sessionID]})
	}
	return out
}

func (b mcpBackend) tabTitle(id string) string {
	b.a.mu.Lock()
	defer b.a.mu.Unlock()
	if m, ok := b.a.tabMeta[id]; ok {
		// A tab without a session takes its title from the remote side.
		return automation.CleanLabel(m.title, maxTabTitle)
	}
	return id
}

const maxTabTitle = 80

func (b mcpBackend) ReadTab(id string, lines int) (string, error) {
	if !b.a.GetSettings().McpReadTabs {
		return "", errors.New("Tabs lesen ist in easyTTY ausgeschaltet (Einstellungen → KI-Zugriff)")
	}
	raw, err := b.a.terms.Tail(id)
	if err != nil {
		return "", err
	}
	if lines <= 0 {
		lines = 60
	}
	lines = min(lines, maxReadLines)
	all := strings.Split(strings.TrimRight(automation.CleanText(raw), "\n "), "\n")
	return strings.Join(all[max(0, len(all)-lines):], "\n"), nil
}

func (b mcpBackend) SendToTab(ctx context.Context, client, id, text string, enter bool) error {
	if _, err := b.a.terms.Conn(id); err != nil {
		return err
	}
	if automation.HasHiddenChars(text) {
		return errors.New("Text mit Steuer- oder unsichtbaren Zeichen wird nicht gesendet (Enter über pressEnter)")
	}
	shown := text
	if enter {
		shown += "  ⏎ (Enter)"
	}
	err := b.a.gate.Approve(ctx, approval.Request{Client: client, Action: "In einen Tab tippen", Target: b.tabTitle(id), Detail: shown,
		GrantKey: "send|" + id + "|" + approval.Digest(text, fmt.Sprint(enter))})
	if err != nil {
		return err
	}
	if enter {
		text += "\r"
	}
	return b.a.terms.Write(id, []byte(text))
}

// resolveSessions accepts ids or unique (case-insensitive) names and returns
// ids plus labels "Name (user@host:port)" for the approval dialog.
func (b mcpBackend) resolveSessions(refs []string) ([]string, []string, error) {
	tree := b.a.store.Tree()
	var ids, labels []string
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		var hits []store.Session
		for _, s := range tree.Sessions {
			if s.ID == ref {
				hits = []store.Session{s}
				break
			}
			if strings.EqualFold(s.Name, ref) {
				hits = append(hits, s)
			}
		}
		switch len(hits) {
		case 0:
			return nil, nil, fmt.Errorf("Session %q nicht gefunden (list_sessions zeigt alle)", ref)
		case 1:
		default:
			return nil, nil, fmt.Errorf("Session-Name %q ist mehrdeutig – bitte die ID aus list_sessions angeben", ref)
		}
		if !slices.Contains(ids, hits[0].ID) {
			ids = append(ids, hits[0].ID)
			labels = append(labels, automation.CleanLabel(fmt.Sprintf("%s (%s)", hits[0].Name, target(hits[0])), 120))
		}
	}
	return ids, labels, nil
}

func (b mcpBackend) RunCommands(ctx context.Context, client string, refs, cmds []string, o mcpserver.RunOptions) (automation.Run, error) {
	if len(refs) > maxMcpSessions || len(cmds) > maxMcpCommands {
		return automation.Run{}, fmt.Errorf("höchstens %d Geräte und %d Befehle pro Aufruf", maxMcpSessions, maxMcpCommands)
	}
	if err := automation.CheckCommands(cmds); err != nil {
		return automation.Run{}, err
	}
	ids, labels, err := b.resolveSessions(refs)
	if err != nil {
		return automation.Run{}, err
	}
	e, err := b.a.engine()
	if err != nil {
		return automation.Run{}, err
	}
	if !(b.a.GetSettings().McpShowAuto && approval.AllReadOnly(cmds)) {
		sorted := slices.Clone(ids)
		slices.Sort(sorted)
		err := b.a.gate.Approve(ctx, approval.Request{Client: client, Action: "Befehle im Tab ausführen (Rückfragen wie [confirm] werden bestätigt)", Target: strings.Join(labels, ", "),
			Detail: strings.Join(cmds, "\n"), GrantKey: "run|" + strings.Join(sorted, ",") + "|" + approval.Digest(cmds...)})
		if err != nil {
			return automation.Run{}, err
		}
	}
	select {
	case b.a.mcpRuns <- struct{}{}:
		defer func() { <-b.a.mcpRuns }()
	case <-ctx.Done():
		return automation.Run{}, ctx.Err()
	}
	// Typed into visible tabs, so the user watches every command and its output.
	key := "mcp-" + fsutil.NewID()
	opts := automation.Options{Device: o.Device, StopOnError: o.StopOnError, AutoConfirm: true}
	b.a.readable(&opts)
	return e.ExecuteWith(ctx, key, ids, cmds, opts,
		automation.Run{TaskName: "Befehle von " + client, Trigger: "MCP: " + client}, b.a.visibleOpener(client, key))
}

func (b mcpBackend) Tasks() ([]mcpserver.TaskInfo, error) {
	views, err := b.a.TaskOverview()
	if err != nil {
		return nil, err
	}
	out := make([]mcpserver.TaskInfo, 0, len(views))
	for _, v := range views {
		ti := mcpserver.TaskInfo{Task: v.Task, When: v.When}
		if v.Next > 0 {
			ti.NextRun = time.UnixMilli(v.Next).Format(time.RFC3339)
		}
		if v.LastRun != nil {
			ti.LastState = v.LastRun.State
		}
		out = append(out, ti)
	}
	return out, nil
}

func yesNo(b bool) string {
	if b {
		return "ja"
	}
	return "nein"
}

func (b mcpBackend) SaveTask(ctx context.Context, client string, t automation.Task) (automation.Task, error) {
	ids, labels, err := b.resolveSessions(t.SessionIDs)
	if err != nil {
		return t, err
	}
	t.SessionIDs = ids
	if t.ID != "" { // the tool has no such field: an update must not reset it
		if old, err := b.task(t.ID); err == nil {
			t.IdleTimeout = old.IdleTimeout
		}
	}
	check := t
	if err := check.Validate(); err != nil {
		return t, err
	}
	action := "Aufgabe anlegen"
	if t.ID != "" {
		action = "Aufgabe ändern"
		t.ModifiedBy = "MCP: " + client
	} else {
		t.CreatedBy = "MCP: " + client
	}
	detail := fmt.Sprintf("Name: %s\nWann: %s (Zeitplan aktiv: %s)\nGerät: %s · bei Fehler stoppen: %s · Rückfragen automatisch bestätigen: %s\n\n%s",
		check.Name, check.Schedule.Describe(), yesNo(t.Enabled), check.Device, yesNo(t.StopOnError), yesNo(t.AutoConfirm),
		strings.Join(automation.SplitCommands(t.Commands), "\n"))
	if err := b.a.gate.Approve(ctx, approval.Request{Client: client, Action: action, Target: strings.Join(labels, ", "), Detail: detail}); err != nil {
		return t, err
	}
	saved, err := b.a.SaveTask(t)
	if err == nil {
		b.a.emit(EventTaskRun, automation.Run{TaskID: saved.ID, TaskName: saved.Name}) // refresh an open list
	}
	return saved, err
}

func (b mcpBackend) task(id string) (automation.Task, error) {
	e, err := b.a.engine()
	if err != nil {
		return automation.Task{}, err
	}
	return e.Store.Task(id)
}

func (b mcpBackend) DeleteTask(ctx context.Context, client, id string) error {
	t, err := b.task(id)
	if err != nil {
		return err
	}
	if err := b.a.gate.Approve(ctx, approval.Request{Client: client, Action: "Aufgabe löschen", Target: t.Name, Detail: t.Schedule.Describe()}); err != nil {
		return err
	}
	return b.a.DeleteTask(id)
}

func (b mcpBackend) RunTask(ctx context.Context, client, id string) (automation.Run, error) {
	t, err := b.task(id)
	if err != nil {
		return automation.Run{}, err
	}
	if err := b.a.gate.Approve(ctx, approval.Request{Client: client, Action: "Aufgabe jetzt ausführen", Target: t.Name, Detail: t.Commands}); err != nil {
		return automation.Run{}, err
	}
	if now, err := b.task(id); err != nil || now.Updated != t.Updated {
		return automation.Run{}, errors.New("die Aufgabe wurde während der Freigabe geändert – bitte erneut anfordern")
	}
	e, err := b.a.engine()
	if err != nil {
		return automation.Run{}, err
	}
	return e.RunTaskWith(ctx, id, "MCP: "+client, b.a.visibleOpener(client, id), b.a.readable)
}

// Stored runs hold device output, so they follow the same switch as reading tabs.
func (b mcpBackend) Runs(taskID string, limit int) ([]automation.Run, error) {
	runs, err := b.a.TaskRuns(taskID, limit)
	if err != nil || b.a.GetSettings().McpReadTabs {
		return runs, err
	}
	out := make([]automation.Run, len(runs))
	for i, r := range runs {
		out[i] = r.Summary()
	}
	return out, nil
}

func (b mcpBackend) Run(id string) (automation.Run, error) {
	r, err := b.a.TaskRun(id)
	if err != nil || b.a.GetSettings().McpReadTabs {
		return r, err
	}
	return r.Summary(), nil
}

/* ---------- lifecycle ---------- */

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// applyMCP starts, restarts or stops the MCP endpoint to match the settings.
func (a *App) applyMCP() {
	s, err := a.settingsForUpdate()
	if err != nil {
		a.emit(terminal.EventNotify, terminal.Notice{Kind: "err", Message: "MCP: " + err.Error()})
		return
	}
	if s.McpEnabled && s.McpToken == "" {
		saved, err := a.updateSettings(func(cur store.Settings) (store.Settings, error) {
			if cur.McpToken == "" { // another writer may have made one meanwhile
				cur.McpToken = randomToken()
			}
			return cur, nil
		})
		if err == nil {
			s = saved
		}
	}
	a.mu.Lock()
	cur, curPort, curToken := a.mcp, a.mcpPort, a.mcpToken
	a.mu.Unlock()
	if cur != nil && s.McpEnabled && curPort == s.McpPort && curToken == s.McpToken {
		return
	}
	if cur != nil {
		_ = cur.Close()
	}
	var l *mcpserver.Listener
	err = nil
	if s.McpEnabled {
		l, err = mcpserver.Listen(mcpserver.NewServer(mcpBackend{a}, appVersion), s.McpPort, s.McpToken)
		if err != nil {
			a.emit(terminal.EventNotify, terminal.Notice{Kind: "err", Message: "MCP: " + err.Error()})
		}
	}
	a.mu.Lock()
	a.mcp, a.mcpPort, a.mcpToken, a.mcpErr = l, s.McpPort, s.McpToken, err
	a.mu.Unlock()
}

// McpInfo is what the settings dialog shows.
type McpInfo struct {
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	Error   string `json:"error,omitempty"`
	Exe     string `json:"exe"`
}

// GetMcpInfo returns state, URL and token for setting up AI clients.
func (a *App) GetMcpInfo() McpInfo {
	s := a.GetSettings()
	exe, _ := os.Executable()
	a.mu.Lock()
	defer a.mu.Unlock()
	info := McpInfo{Enabled: s.McpEnabled, Running: a.mcp != nil, Token: s.McpToken, Exe: exe,
		URL: fmt.Sprintf("http://127.0.0.1:%d%s", s.McpPort, mcpserver.Endpoint)}
	if a.mcpErr != nil {
		info.Error = a.mcpErr.Error()
	}
	return info
}

// NewMcpToken replaces the token (old client configs stop working).
func (a *App) NewMcpToken() (McpInfo, error) {
	if _, err := a.updateSettings(func(cur store.Settings) (store.Settings, error) {
		cur.McpToken = randomToken()
		return cur, nil
	}); err != nil {
		return McpInfo{}, err
	}
	a.applyMCP()
	return a.GetMcpInfo(), nil
}

// stopMCP closes the endpoint (shutdown).
func (a *App) stopMCP() {
	a.mu.Lock()
	l := a.mcp
	a.mcp = nil
	a.mu.Unlock()
	if l != nil {
		_ = l.Close()
	}
}
