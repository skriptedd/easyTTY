package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"easytty/internal/automation"
)

type fakeBackend struct {
	mu      sync.Mutex
	clients []string
	deny    bool
	sent    string
	saved   automation.Task
}

func (f *fakeBackend) approve(client string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients = append(f.clients, client)
	if f.deny {
		return ErrDenied
	}
	return nil
}

func (f *fakeBackend) Sessions() []SessionInfo {
	return []SessionInfo{{ID: "s1", Name: "Core-sw1", Folder: "Rechenzentrum", Protocol: "ssh", Target: "admin@10.0.0.1:22", HasCredentials: true}}
}
func (f *fakeBackend) Tabs() []TabInfo {
	return []TabInfo{{TabID: "t1", Title: "Core-sw1", SessionID: "s1"}}
}
func (f *fakeBackend) ReadTab(id string, lines int) (string, error) {
	if id != "t1" {
		return "", errors.New("Tab nicht verbunden")
	}
	return "sw1#show clock\n12:00:00", nil
}
func (f *fakeBackend) SendToTab(ctx context.Context, client, id, text string, enter bool) error {
	if err := f.approve(client); err != nil {
		return err
	}
	f.sent = text
	return nil
}
func (f *fakeBackend) RunCommands(ctx context.Context, client string, sessions, cmds []string, o RunOptions) (automation.Run, error) {
	if err := f.approve(client); err != nil {
		return automation.Run{}, err
	}
	r := automation.Run{ID: "r1", TaskName: "MCP", State: automation.RunOK}
	for _, s := range sessions {
		sr := automation.SessionRun{SessionID: s, Session: s}
		for _, c := range cmds {
			sr.Result.Steps = append(sr.Result.Steps, automation.Step{Command: c, Output: "out of " + c})
		}
		r.Sessions = append(r.Sessions, sr)
	}
	return r, nil
}
func (f *fakeBackend) Tasks() ([]TaskInfo, error) {
	return []TaskInfo{{Task: automation.Task{ID: "k1", Name: "Backup"}, When: "täglich um 02:00"}}, nil
}
func (f *fakeBackend) SaveTask(ctx context.Context, client string, t automation.Task) (automation.Task, error) {
	if err := f.approve(client); err != nil {
		return t, err
	}
	t.ID = "new"
	f.saved = t
	return t, nil
}
func (f *fakeBackend) DeleteTask(ctx context.Context, client, id string) error {
	return f.approve(client)
}
func (f *fakeBackend) RunTask(ctx context.Context, client, id string) (automation.Run, error) {
	return automation.Run{ID: "r2"}, f.approve(client)
}
func (f *fakeBackend) Runs(taskID string, limit int) ([]automation.Run, error) {
	return []automation.Run{{ID: "r1", TaskID: taskID}}, nil
}
func (f *fakeBackend) Run(id string) (automation.Run, error) { return automation.Run{ID: id}, nil }

func connect(t *testing.T, srv *mcp.Server, name string) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: name, Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args any) (map[string]any, *mcp.CallToolResult) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out map[string]any
	if res.StructuredContent != nil {
		raw, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(raw, &out)
	}
	return out, res
}

func TestToolsInMemory(t *testing.T) {
	fb := &fakeBackend{}
	cs := connect(t, NewServer(fb, "test"), "Claude Code")
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 11 {
		t.Fatalf("tools: %v %d", err, len(tools.Tools))
	}
	for _, tl := range tools.Tools {
		ro := tl.Annotations != nil && tl.Annotations.ReadOnlyHint
		wantRO := strings.HasPrefix(tl.Name, "list_") || strings.HasPrefix(tl.Name, "get_") || tl.Name == "read_tab"
		if ro != wantRO {
			t.Errorf("%s readOnly=%v", tl.Name, ro)
		}
	}
	out, _ := call(t, cs, "list_sessions", map[string]any{})
	if !strings.Contains(mustJSON(out), "Core-sw1") || strings.Contains(strings.ToLower(mustJSON(out)), "password\"") {
		t.Errorf("list_sessions: %v", out)
	}
	out, _ = call(t, cs, "read_tab", map[string]any{"tabId": "t1"})
	if out["text"] != "sw1#show clock\n12:00:00" {
		t.Errorf("read_tab: %v", out)
	}
	_, res := call(t, cs, "read_tab", map[string]any{"tabId": "nope"})
	if !res.IsError {
		t.Error("read_tab unknown tab must be an error result")
	}
	out, _ = call(t, cs, "run_commands", map[string]any{"sessions": []string{"Core-sw1"}, "commands": "show version\n! skip\nshow clock"})
	if !strings.Contains(mustJSON(out), "out of show clock") || strings.Contains(mustJSON(out), "skip") {
		t.Errorf("run_commands: %v", out)
	}
	_, res = call(t, cs, "run_commands", map[string]any{"sessions": []string{}, "commands": ""})
	if !res.IsError {
		t.Error("empty run must fail")
	}
	call(t, cs, "save_task", map[string]any{"name": "Nacht", "sessions": []string{"s1"}, "commands": "show run",
		"schedule": map[string]any{"kind": "daily", "at": "02:00"}})
	if !fb.saved.Enabled || !fb.saved.StopOnError || !fb.saved.AutoConfirm || fb.saved.Schedule.At != "02:00" {
		t.Errorf("save_task defaults: %+v", fb.saved)
	}
	if fb.clients[0] != "Claude Code" {
		t.Errorf("client name %v", fb.clients)
	}
	fb.deny = true
	_, res = call(t, cs, "send_to_tab", map[string]any{"tabId": "t1", "text": "reload"})
	if !res.IsError || !strings.Contains(mustJSON(res.Content), "abgelehnt") || fb.sent != "" {
		t.Errorf("denied send: %+v", res)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestHTTPRequiresToken(t *testing.T) {
	l, err := Listen(NewServer(&fakeBackend{}, "test"), 0, "geheim123")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !strings.HasPrefix(l.URL, "http://127.0.0.1:") {
		t.Fatalf("url %s", l.URL)
	}
	for _, auth := range []string{"", "Bearer falsch", "geheim123"} {
		req, _ := http.NewRequest("POST", l.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("auth %q: %d", auth, resp.StatusCode)
		}
	}
	// Disabled = empty token never authenticates.
	h := Handler(NewServer(&fakeBackend{}, "t"), "")
	rec := &statusRecorder{header: http.Header{}}
	req, _ := http.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer ")
	h.ServeHTTP(rec, req)
	if rec.code != http.StatusUnauthorized {
		t.Errorf("empty token: %d", rec.code)
	}
}

type statusRecorder struct {
	header http.Header
	code   int
}

func (r *statusRecorder) Header() http.Header         { return r.header }
func (r *statusRecorder) Write(p []byte) (int, error) { return len(p), nil }
func (r *statusRecorder) WriteHeader(c int)           { r.code = c }

func TestBridgeForwardsToolsAndClientName(t *testing.T) {
	fb := &fakeBackend{}
	l, err := Listen(NewServer(fb, "test"), 0, "tok")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ct, st := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- Bridge(ctx, l.URL, "tok", st) }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "Claude Desktop", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 11 {
		t.Fatalf("bridge tools: %v", err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "send_to_tab", Arguments: map[string]any{"tabId": "t1", "text": "show clock", "pressEnter": true}})
	if err != nil || res.IsError || fb.sent != "show clock" {
		t.Fatalf("bridge call: %v %+v", err, res)
	}
	if fb.clients[0] != "Claude Desktop" {
		t.Errorf("client via bridge = %v", fb.clients)
	}
	cs.Close()

	if err := Bridge(ctx, l.URL, "falsch", st); err == nil {
		t.Error("bridge with wrong token must fail")
	}
}

func TestBridgeOnlyLoopback(t *testing.T) {
	_, st := mcp.NewInMemoryTransports()
	for _, u := range []string{"http://evil.example:47321/mcp", "http://192.168.1.5:47321/mcp", "::nope"} {
		if err := Bridge(context.Background(), u, "tok", st); err == nil || !strings.Contains(err.Error(), "nur zu easyTTY") {
			t.Errorf("%s: %v", u, err)
		}
	}
}

func TestClientNameSanitized(t *testing.T) {
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{MetaClient: "Evil\nWann: nie\u202e" + strings.Repeat("x", 100)}}}
	got := clientName(req)
	if strings.ContainsAny(got, "\n\u202e") || len([]rune(got)) > 61 {
		t.Errorf("clientName = %q", got)
	}
}
