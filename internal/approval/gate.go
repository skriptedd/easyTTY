// Package approval is the gate between AI clients and the user: nothing that
// changes a device or a task passes without a click in easyTTY. It knows no UI;
// a Notifier shows the question and Answer brings the reply back.
package approval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout = 3 * time.Minute
	defaultGrant   = 15 * time.Minute
	maxPending     = 5 // open approval dialogs at once
)

// Answers the UI can give; anything else counts as a denial.
const (
	AnswerOnce  = "once"  // this one time
	AnswerGrant = "grant" // this exact action, for the grant period
)

var (
	// ErrDenied means the user said no or did not answer in time.
	ErrDenied = errors.New("vom Benutzer in easyTTY abgelehnt")
	// ErrTooManyPending protects the user from a flood of dialogs.
	ErrTooManyPending = errors.New("zu viele offene Anfragen in easyTTY – erst die offenen beantworten")
)

// Request is one action to approve. GrantKey "" means: ask every time.
type Request struct {
	Client, Action, Target, Detail string
	GrantKey                       string
}

// Event asks the user whether an AI client may do something.
type Event struct {
	ID        string `json:"id"`
	Client    string `json:"client"`
	Action    string `json:"action"` // human readable
	Target    string `json:"target"`
	Detail    string `json:"detail"` // commands, task definition …
	Grantable bool   `json:"grantable"`
	Timeout   int    `json:"timeout"`
}

// Notifier connects the gate to a user interface.
type Notifier interface {
	Ask(Event)           // show the question
	Done(id string)      // answered
	Cancelled(id string) // gone unanswered (timeout, client left)
}

// Gate holds pending questions and the short-lived grants.
type Gate struct {
	n       Notifier
	newID   func() string
	now     func() time.Time
	timeout time.Duration
	grant   time.Duration

	mu      sync.Mutex
	pending map[string]chan string
	grants  map[string]time.Time
}

// NewGate returns a gate; newID makes ids for the questions.
func NewGate(n Notifier, newID func() string) *Gate {
	return &Gate{n: n, newID: newID, now: time.Now, timeout: defaultTimeout, grant: defaultGrant,
		pending: map[string]chan string{}, grants: map[string]time.Time{}}
}

// Approve blocks until the user allows (nil) or denies (error). Grants are
// bound to the client and the key, which the caller derives from the action.
func (g *Gate) Approve(ctx context.Context, r Request) error {
	key := ""
	if r.GrantKey != "" {
		key = r.Client + "|" + r.GrantKey
	}
	id, ch, err := g.enqueue(key)
	if err != nil || ch == nil {
		return err
	}
	defer g.forget(id)
	g.n.Ask(Event{ID: id, Client: r.Client, Action: r.Action, Target: r.Target, Detail: r.Detail,
		Grantable: key != "", Timeout: int(g.timeout.Seconds())})
	var answer string
	select {
	case answer = <-ch:
		g.n.Done(id)
	case <-time.After(g.timeout):
		g.n.Cancelled(id)
		return fmt.Errorf("%w (keine Antwort innerhalb von %d Minuten)", ErrDenied, int(g.timeout.Minutes()))
	case <-ctx.Done():
		g.n.Cancelled(id)
		return ctx.Err()
	}
	switch {
	case answer == AnswerGrant && key != "":
		g.remember(key)
		return nil
	case answer == AnswerOnce || answer == AnswerGrant:
		return nil
	}
	return ErrDenied
}

// enqueue returns a nil channel when a grant already covers the request.
func (g *Gate) enqueue(key string) (string, chan string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if until, ok := g.grants[key]; key != "" && ok && g.now().Before(until) {
		return "", nil, nil
	}
	if len(g.pending) >= maxPending {
		return "", nil, ErrTooManyPending
	}
	id := g.newID()
	ch := make(chan string, 1)
	g.pending[id] = ch
	return id, ch, nil
}

func (g *Gate) forget(id string) {
	g.mu.Lock()
	delete(g.pending, id)
	g.mu.Unlock()
}

func (g *Gate) remember(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	for k, until := range g.grants { // drop expired grants
		if now.After(until) {
			delete(g.grants, k)
		}
	}
	g.grants[key] = now.Add(g.grant)
}

// Answer delivers the user's reply to a pending question.
func (g *Gate) Answer(id, answer string) {
	g.mu.Lock()
	ch := g.pending[id]
	g.mu.Unlock()
	if ch != nil {
		select {
		case ch <- answer:
		default:
		}
	}
}

// Digest fingerprints the content of an action for a grant key, so a grant
// never covers different commands.
func Digest(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:8])
}
