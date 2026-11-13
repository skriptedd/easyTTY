// Package mcpserver exposes easyTTY to AI assistants via the Model Context
// Protocol: list sessions and open tabs, read terminal output, run commands
// on devices, manage scheduled tasks. Everything that reaches a device goes
// through Backend, which asks the user for approval in easyTTY.
package mcpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"easytty/internal/approval"
	"easytty/internal/automation"
)

// MetaClient is set by the stdio bridge so approvals name the real client.
const MetaClient = "easytty/client"

// SessionInfo describes a saved session (no secrets).
type SessionInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Folder         string `json:"folder" jsonschema:"folder path, empty = top level"`
	Protocol       string `json:"protocol" jsonschema:"ssh or serial"`
	Target         string `json:"target" jsonschema:"user@host:port or COM port"`
	HasCredentials bool   `json:"hasCredentials" jsonschema:"true if commands can run without a login prompt"`
}

// TabInfo describes an open terminal tab.
type TabInfo struct {
	TabID     string `json:"tabId"`
	Title     string `json:"title"`
	SessionID string `json:"sessionId,omitempty"`
	Folder    string `json:"folder,omitempty"`
}

// TaskInfo is a task with its schedule state.
type TaskInfo struct {
	Task      automation.Task `json:"task"`
	When      string          `json:"when"`
	NextRun   string          `json:"nextRun,omitempty"`
	LastState string          `json:"lastState,omitempty"`
}

// RunOptions tune run_commands.
type RunOptions struct {
	Device      string
	StopOnError bool
}

// Backend is implemented by the app. Methods taking client must ask the
// user for approval (and return an error if denied).
type Backend interface {
	Sessions() []SessionInfo
	Tabs() []TabInfo
	ReadTab(tabID string, lines int) (string, error)
	SendToTab(ctx context.Context, client, tabID, text string, enter bool) error
	RunCommands(ctx context.Context, client string, sessions []string, commands []string, opts RunOptions) (automation.Run, error)
	Tasks() ([]TaskInfo, error)
	SaveTask(ctx context.Context, client string, t automation.Task) (automation.Task, error)
	DeleteTask(ctx context.Context, client, id string) error
	RunTask(ctx context.Context, client, id string) (automation.Run, error)
	Runs(taskID string, limit int) ([]automation.Run, error)
	Run(id string) (automation.Run, error)
}

// clientName returns the MCP client's name (from the bridge meta or
// initialize), without control characters so it cannot forge dialog lines.
func clientName(req *mcp.CallToolRequest) string {
	return automation.CleanLabel(rawClientName(req), 60)
}

func rawClientName(req *mcp.CallToolRequest) string {
	if req.Params != nil {
		if n, ok := req.Params.Meta[MetaClient].(string); ok && n != "" {
			return n
		}
	}
	if req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil && p.ClientInfo.Name != "" {
			return p.ClientInfo.Name
		}
	}
	return "unbekannter KI-Client"
}

var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}

func boolPtr(b bool) *bool { return &b }

var changes = &mcp.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(true)}

type empty struct{}

type sessionsOut struct {
	Sessions []SessionInfo `json:"sessions"`
}
type tabsOut struct {
	Tabs []TabInfo `json:"tabs"`
}
type readTabIn struct {
	TabID string `json:"tabId" jsonschema:"tab id from list_tabs"`
	Lines int    `json:"lines,omitempty" jsonschema:"how many of the last lines (default 60, max 2000)"`
}
type textOut struct {
	Text string `json:"text"`
}
type sendIn struct {
	TabID      string `json:"tabId" jsonschema:"tab id from list_tabs"`
	Text       string `json:"text" jsonschema:"text to type into the terminal"`
	PressEnter bool   `json:"pressEnter,omitempty" jsonschema:"send Enter after the text"`
}
type okOut struct {
	OK bool `json:"ok"`
}
type runIn struct {
	Sessions    []string `json:"sessions" jsonschema:"session ids or names (see list_sessions); run one after another"`
	Commands    string   `json:"commands" jsonschema:"one command per line, as typed on the device; lines starting with ! or # are skipped"`
	Device      string   `json:"device,omitempty" jsonschema:"auto (default), cisco or other"`
	StopOnError *bool    `json:"stopOnError,omitempty" jsonschema:"skip the rest on a device after an error (default true)"`
}
type tasksOut struct {
	Tasks []TaskInfo `json:"tasks"`
}
type saveTaskIn struct {
	ID          string              `json:"id,omitempty" jsonschema:"id of an existing task to update; empty = new task"`
	Name        string              `json:"name"`
	Sessions    []string            `json:"sessions" jsonschema:"session ids or names"`
	Commands    string              `json:"commands" jsonschema:"one command per line"`
	Schedule    automation.Schedule `json:"schedule" jsonschema:"kind: manual|once|daily|weekly|interval; date YYYY-MM-DD (once); at HH:MM; days 1=Mon..7=Sun (weekly); everyMin (interval, >= 5)"`
	Enabled     *bool               `json:"enabled,omitempty" jsonschema:"schedule active (default true)"`
	Device      string              `json:"device,omitempty" jsonschema:"auto, cisco or other"`
	StopOnError *bool               `json:"stopOnError,omitempty"`
	AutoConfirm *bool               `json:"autoConfirm,omitempty" jsonschema:"answer [confirm] questions with Enter (default true)"`
}
type idIn struct {
	ID string `json:"id"`
}
type runsIn struct {
	TaskID string `json:"taskId,omitempty" jsonschema:"only runs of this task"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max runs (default 20)"`
}
type runsOut struct {
	Runs []automation.Run `json:"runs"`
}
type runIDIn struct {
	RunID string `json:"runId"`
}

func orTrue(b *bool) bool { return b == nil || *b }

// NewServer builds the MCP server with all easyTTY tools.
func NewServer(b Backend, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "easyTTY", Title: "easyTTY Terminal", Version: version}, &mcp.ServerOptions{
		Instructions: "easyTTY is the user's SSH/serial terminal for network devices (mostly Cisco switches) and servers. " +
			"Use list_sessions/list_tabs first. Reading is free (read_tab only if the user allowed it); anything sent to a device (run_commands, send_to_tab, run_task, save_task, delete_task) " +
			"must be approved by the user in easyTTY, so batch commands and explain why. Prefer show/display commands; do not change configurations unless asked.",
	})

	mcp.AddTool(s, &mcp.Tool{Name: "list_sessions", Description: "Saved sessions (devices) with folder, protocol and target. No passwords.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, sessionsOut, error) {
			return nil, sessionsOut{Sessions: b.Sessions()}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_tabs", Description: "Terminal tabs currently open in easyTTY.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, tabsOut, error) {
			return nil, tabsOut{Tabs: b.Tabs()}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "read_tab", Description: "Recent output of an open tab as plain text (what the user sees).", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in readTabIn) (*mcp.CallToolResult, textOut, error) {
			text, err := b.ReadTab(in.TabID, in.Lines)
			return nil, textOut{Text: text}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "send_to_tab", Description: "Type text into an open tab (the user must approve). Use run_commands to collect output.", Annotations: changes},
		func(ctx context.Context, req *mcp.CallToolRequest, in sendIn) (*mcp.CallToolResult, okOut, error) {
			err := b.SendToTab(ctx, clientName(req), in.TabID, in.Text, in.PressEnter)
			return nil, okOut{OK: err == nil}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "run_commands", Description: "Run commands on one or more saved sessions and return each command's output. They are typed into a visible easyTTY tab (an open tab of the session is reused, otherwise one is opened), so the user watches live. The user must approve.", Annotations: changes},
		func(ctx context.Context, req *mcp.CallToolRequest, in runIn) (*mcp.CallToolResult, automation.Run, error) {
			cmds := automation.SplitCommands(in.Commands)
			if len(cmds) == 0 || len(in.Sessions) == 0 {
				return nil, automation.Run{}, errors.New("sessions and commands are required")
			}
			r, err := b.RunCommands(ctx, clientName(req), in.Sessions, cmds, RunOptions{Device: in.Device, StopOnError: orTrue(in.StopOnError)})
			return nil, r, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_tasks", Description: "Saved tasks (command lists with schedules) with next run and last result.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, tasksOut, error) {
			t, err := b.Tasks()
			return nil, tasksOut{Tasks: t}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "save_task", Description: "Create or update a task, e.g. a nightly backup or a change in a maintenance window. The user must approve.", Annotations: changes},
		func(ctx context.Context, req *mcp.CallToolRequest, in saveTaskIn) (*mcp.CallToolResult, automation.Task, error) {
			t := automation.Task{ID: in.ID, Name: in.Name, SessionIDs: in.Sessions, Commands: in.Commands, Schedule: in.Schedule,
				Enabled: orTrue(in.Enabled), Device: in.Device, StopOnError: orTrue(in.StopOnError), AutoConfirm: orTrue(in.AutoConfirm)}
			saved, err := b.SaveTask(ctx, clientName(req), t)
			return nil, saved, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_task", Description: "Delete a task (history stays). The user must approve.", Annotations: changes},
		func(ctx context.Context, req *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, okOut, error) {
			err := b.DeleteTask(ctx, clientName(req), in.ID)
			return nil, okOut{OK: err == nil}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "run_task", Description: "Run a saved task now and wait for the result. The user must approve.", Annotations: changes},
		func(ctx context.Context, req *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, automation.Run, error) {
			r, err := b.RunTask(ctx, clientName(req), in.ID)
			return nil, r, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_runs", Description: "Past task runs (newest first, without output; use get_run for output).", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in runsIn) (*mcp.CallToolResult, runsOut, error) {
			if in.Limit <= 0 || in.Limit > 200 {
				in.Limit = 20
			}
			r, err := b.Runs(in.TaskID, in.Limit)
			return nil, runsOut{Runs: r}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_run", Description: "One task run with the output of every command.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in runIDIn) (*mcp.CallToolResult, automation.Run, error) {
			r, err := b.Run(in.RunID)
			return nil, r, err
		})
	return s
}

// Endpoint is the URL path of the HTTP transport.
const Endpoint = "/mcp"

// Handler serves srv over streamable HTTP and requires "Authorization: Bearer <token>".
func Handler(srv *mcp.Server, token string) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{SessionTimeout: 30 * time.Minute})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, isBearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !isBearer || token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="easyTTY"`)
			http.Error(w, "easyTTY: Token fehlt oder falsch", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Listener serves the MCP endpoint on 127.0.0.1 only.
type Listener struct {
	srv *http.Server
	URL string
}

// Listen starts the HTTP transport on 127.0.0.1:port.
func Listen(srv *mcp.Server, port int, token string) (*Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("MCP-Port %d: %w", port, err)
	}
	mux := http.NewServeMux()
	mux.Handle(Endpoint, Handler(srv, token))
	hs := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = hs.Serve(ln) }()
	return &Listener{srv: hs, URL: fmt.Sprintf("http://127.0.0.1:%d%s", ln.Addr().(*net.TCPAddr).Port, Endpoint)}, nil
}

// Close stops the listener.
func (l *Listener) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return l.srv.Shutdown(ctx)
}

type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

// Bridge connects to a running easyTTY over HTTP and serves the same tools on
// t (stdio for clients that only start programs, e.g. Claude Desktop).
func Bridge(ctx context.Context, url, token string, t mcp.Transport) error {
	if u, err := neturl.Parse(url); err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		return fmt.Errorf("Brücke verbindet nur zu easyTTY auf diesem PC (127.0.0.1), nicht zu %q", url)
	}
	upstream := mcp.NewClient(&mcp.Implementation{Name: "easyTTY-bridge", Version: "1"}, nil)
	cs, err := upstream.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: &http.Client{Transport: bearer{token: token, base: http.DefaultTransport}},
	}, nil)
	if err != nil {
		return fmt.Errorf("easyTTY nicht erreichbar unter %s (läuft easyTTY und ist MCP eingeschaltet?): %w", url, err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "easyTTY", Version: "bridge"}, nil)
	for _, tool := range tools.Tools {
		name := tool.Name
		srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			meta := mcp.Meta{MetaClient: clientName(req)}
			return cs.CallTool(ctx, &mcp.CallToolParams{Meta: meta, Name: name, Arguments: json.RawMessage(req.Params.Arguments)})
		})
	}
	return srv.Run(ctx, t)
}

// ErrDenied is returned by backends when the user declines.
var ErrDenied = approval.ErrDenied

var _ io.Closer = (*Listener)(nil)
