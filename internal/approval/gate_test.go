package approval

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeUI answers every question with a fixed reply ("" = leaves it open).
type fakeUI struct {
	gate   *Gate
	answer string

	mu        sync.Mutex
	asked     []Event
	done      []string
	cancelled []string
}

func (f *fakeUI) Ask(e Event) {
	f.mu.Lock()
	f.asked = append(f.asked, e)
	f.mu.Unlock()
	if f.answer != "" {
		go f.gate.Answer(e.ID, f.answer)
	}
}
func (f *fakeUI) Done(id string) { f.mu.Lock(); f.done = append(f.done, id); f.mu.Unlock() }
func (f *fakeUI) Cancelled(id string) {
	f.mu.Lock()
	f.cancelled = append(f.cancelled, id)
	f.mu.Unlock()
}
func (f *fakeUI) questions() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.asked) }

func newGate(answer string) (*Gate, *fakeUI) {
	n := 0
	ui := &fakeUI{answer: answer}
	var mu sync.Mutex
	g := NewGate(ui, func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprint("q", n)
	})
	ui.gate = g
	return g, ui
}

func TestAnswers(t *testing.T) {
	for answer, want := range map[string]error{AnswerOnce: nil, AnswerGrant: nil, "deny": ErrDenied, "quatsch": ErrDenied} {
		g, ui := newGate(answer)
		err := g.Approve(context.Background(), Request{Client: "c", Action: "a", GrantKey: "k"})
		if !errors.Is(err, want) {
			t.Errorf("%s: %v", answer, err)
		}
		if len(ui.done) != 1 || !ui.asked[0].Grantable {
			t.Errorf("%s: done %v, event %+v", answer, ui.done, ui.asked)
		}
	}
}

func TestGrantCoversOnlyTheSameClientAndAction(t *testing.T) {
	g, ui := newGate(AnswerGrant)
	ctx := context.Background()
	if err := g.Approve(ctx, Request{Client: "c", GrantKey: "run|a"}); err != nil {
		t.Fatal(err)
	}
	if err := g.Approve(ctx, Request{Client: "c", GrantKey: "run|a"}); err != nil || ui.questions() != 1 {
		t.Fatalf("same action must pass silently: %v, %d questions", err, ui.questions())
	}
	_ = g.Approve(ctx, Request{Client: "c", GrantKey: "run|b"})
	_ = g.Approve(ctx, Request{Client: "other", GrantKey: "run|a"})
	if ui.questions() != 3 {
		t.Fatalf("other action or client must ask again: %d questions", ui.questions())
	}
}

func TestGrantExpires(t *testing.T) {
	g, ui := newGate(AnswerGrant)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }
	ctx := context.Background()
	_ = g.Approve(ctx, Request{Client: "c", GrantKey: "k"})
	now = now.Add(defaultGrant - time.Second)
	_ = g.Approve(ctx, Request{Client: "c", GrantKey: "k"})
	if ui.questions() != 1 {
		t.Fatal("grant ended too early")
	}
	now = now.Add(2 * time.Second)
	_ = g.Approve(ctx, Request{Client: "c", GrantKey: "k"})
	if ui.questions() != 2 {
		t.Fatal("grant must expire")
	}
}

func TestRequestWithoutGrantKeyAlwaysAsks(t *testing.T) {
	g, ui := newGate(AnswerGrant)
	ctx := context.Background()
	_ = g.Approve(ctx, Request{Client: "c"})
	_ = g.Approve(ctx, Request{Client: "c"})
	if ui.questions() != 2 || ui.asked[0].Grantable {
		t.Fatalf("%d questions, grantable %v", ui.questions(), ui.asked[0].Grantable)
	}
}

func TestTimeoutDenies(t *testing.T) {
	g, ui := newGate("")
	g.timeout = 10 * time.Millisecond
	err := g.Approve(context.Background(), Request{Client: "c"})
	if !errors.Is(err, ErrDenied) || len(ui.cancelled) != 1 {
		t.Fatalf("%v, cancelled %v", err, ui.cancelled)
	}
	if len(g.pending) != 0 {
		t.Fatal("question left pending")
	}
}

func TestClientLeavingCancelsTheQuestion(t *testing.T) {
	g, ui := newGate("")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- g.Approve(ctx, Request{Client: "c"}) }()
	for ui.questions() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) || len(ui.cancelled) != 1 {
		t.Fatalf("%v, cancelled %v", err, ui.cancelled)
	}
}

func TestTooManyOpenQuestions(t *testing.T) {
	g, ui := newGate("")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range maxPending {
		go func() { _ = g.Approve(ctx, Request{Client: "c"}) }()
	}
	for ui.questions() < maxPending {
		time.Sleep(time.Millisecond)
	}
	if err := g.Approve(ctx, Request{Client: "c"}); !errors.Is(err, ErrTooManyPending) {
		t.Fatalf("got %v", err)
	}
}

func TestAnswerForUnknownQuestionIsIgnored(t *testing.T) {
	g, _ := newGate("")
	g.Answer("nobody", AnswerOnce)
}

func TestDigestDiffers(t *testing.T) {
	if Digest("show clock") == Digest("reload") || Digest("a", "b") == Digest("ab") {
		t.Fatal("digest collision")
	}
}
