package main

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"easytty/internal/automation"
	"easytty/internal/store"
	"easytty/internal/terminal"
)

const (
	serialLoginWindow = 30 * time.Second // console prompts are answered only this long after connecting
	serialLoginStep   = 10 * time.Second // after the user name, the password prompt must follow quickly
)

var (
	userPrompt = regexp.MustCompile(`(?i)(user ?name|login)\s*:\s*$`)
	passPrompt = regexp.MustCompile(`(?i)password\s*:\s*$`)
)

// serialLogin answers the console's login prompts with the stored user and
// password – only right after connecting (or when asked from the tab menu),
// so a later "Password:" (e.g. after "enable") never gets the login password.
func (a *App) serialLogin(tabID string, s store.Session, wake bool) {
	pw, _ := a.store.Password(s.ID)
	if s.User == "" && pw == "" {
		return
	}
	stream, err := a.terms.Tap(tabID)
	if err != nil {
		return // an automation drives the tab right now
	}
	defer stream.Close()
	ctx, cancel := context.WithTimeout(a.ctx, serialLoginWindow)
	defer cancel()
	switch consoleLogin(ctx, newChanConn(stream), s.User, pw, wake) {
	case loginOK:
		a.emit(terminal.EventNotify, terminal.Notice{Kind: "ok", Message: s.Name + ": automatisch angemeldet" + userSuffix(s.User)})
	case loginRejected:
		a.emit(terminal.EventNotify, terminal.Notice{Kind: "err", Message: s.Name + ": Anmeldung abgelehnt – Benutzer/Passwort in der Session prüfen"})
	}
}

// chanConn reads its connection in a single goroutine, so a login step and
// the commands after it share one reader (a second reader would steal output).
type chanConn struct {
	rw   io.ReadWriteCloser
	ch   chan []byte
	rest []byte
	err  error
}

func newChanConn(rw io.ReadWriteCloser) *chanConn {
	c := &chanConn{rw: rw, ch: make(chan []byte, 64)}
	go func() {
		defer close(c.ch)
		buf := make([]byte, 4096)
		for {
			n, err := rw.Read(buf)
			if n > 0 {
				c.ch <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				c.err = err
				return
			}
		}
	}()
	return c
}

// next waits for the next chunk or ctx.
func (c *chanConn) next(ctx context.Context) ([]byte, bool) {
	select {
	case <-ctx.Done():
		return nil, false
	case b, ok := <-c.ch:
		return b, ok
	}
}

func (c *chanConn) Read(p []byte) (int, error) {
	if len(c.rest) == 0 {
		b, ok := <-c.ch
		if !ok {
			if c.err != nil {
				return 0, c.err
			}
			return 0, io.EOF
		}
		c.rest = b
	}
	n := copy(p, c.rest)
	c.rest = c.rest[n:]
	return n, nil
}

func (c *chanConn) Write(p []byte) (int, error) { return c.rw.Write(p) }
func (c *chanConn) Close() error                { return c.rw.Close() }

// loginIfNeeded logs in on a console that shows a login prompt (AI runs,
// "Jetzt ausführen", scheduled tasks); a console already at its CLI is left alone.
func loginIfNeeded(ctx context.Context, rw io.ReadWriteCloser, s store.Session, pw string) (io.ReadWriteCloser, error) {
	if s.Protocol != store.ProtocolSerial || (s.User == "" && pw == "") {
		return rw, nil
	}
	cc := newChanConn(rw)
	ctx, cancel := context.WithTimeout(ctx, serialLoginWindow)
	defer cancel()
	if consoleLogin(ctx, cc, s.User, pw, true) == loginRejected {
		return cc, errors.New(s.Name + ": Anmeldung an der Konsole abgelehnt – Benutzer/Passwort in der Session prüfen")
	}
	return cc, nil
}

type loginResult int

const (
	loginNone     loginResult = iota // no login prompt (console without login) or nothing to send
	loginOK                          // a CLI prompt followed the password
	loginRejected                    // the device asked again or reported a failure
)

var (
	cliPrompt   = regexp.MustCompile(`^[\w.\-@:~/()\[\] ]{1,100}[#>$%]\s*$`)
	loginFailed = regexp.MustCompile(`(?i)(login invalid|authentication failed|access denied|login incorrect|bad password)`)
)

// consoleLogin drives one login on rw until ctx ends: user name on the user
// prompt, password on the password prompt (also without a user prompt, for
// line passwords), then waits for a CLI prompt or a rejection.
func consoleLogin(ctx context.Context, rw *chanConn, user, pw string, wake bool) loginResult {
	if wake {
		if _, err := rw.Write([]byte("\r")); err != nil {
			return loginNone
		}
	}
	var seen []byte
	sentUser, sentPw := false, false
	var stepDeadline time.Time
	for {
		p, ok := rw.next(ctx)
		if !ok {
			if sentPw && ctx.Err() != nil {
				return loginOK // password accepted without complaint, the device just stayed quiet
			}
			return loginNone
		}
		seen = append(seen, p...)
		if len(seen) > 4096 {
			seen = seen[len(seen)-4096:]
		}
		text := automation.CleanText(seen)
		last := strings.TrimRight(text[strings.LastIndexByte(text, '\n')+1:], " ")
		if sentPw {
			switch {
			case loginFailed.MatchString(text), userPrompt.MatchString(last), passPrompt.MatchString(last):
				return loginRejected
			case cliPrompt.MatchString(last):
				return loginOK
			}
			continue
		}
		switch {
		case userPrompt.MatchString(last) && sentUser:
			return loginRejected // asked for the user again: the user name was refused
		case userPrompt.MatchString(last) && user != "":
			sentUser, seen, stepDeadline = true, nil, time.Now().Add(serialLoginStep)
			if _, err := rw.Write([]byte(user + "\r")); err != nil {
				return loginNone
			}
		case passPrompt.MatchString(last) && pw != "":
			if sentUser && time.Now().After(stepDeadline) {
				return loginNone // too late after the user name: not our login prompt
			}
			sentPw, seen = true, nil
			if _, err := rw.Write([]byte(pw + "\r")); err != nil {
				return loginNone
			}
		case cliPrompt.MatchString(last) && !sentUser:
			return loginNone // console without login
		}
	}
}

func userSuffix(u string) string {
	if u == "" {
		return ""
	}
	return " als " + u
}

// SerialLogin types the stored login into a serial tab again (tab menu), e.g.
// after the console logged out on idle.
func (a *App) SerialLogin(tabID string) error {
	a.mu.Lock()
	m, ok := a.tabMeta[tabID]
	a.mu.Unlock()
	if !ok || m.sessionID == "" {
		return errors.New("nur für gespeicherte Sessions")
	}
	s, err := a.store.Session(m.sessionID)
	if err != nil {
		return err
	}
	if s.Protocol != store.ProtocolSerial {
		return errors.New("nur für serielle Sessions")
	}
	if s.User == "" && !s.HasPassword {
		return errors.New("in der Session ist kein Benutzer/Passwort hinterlegt")
	}
	go a.serialLogin(tabID, s, true)
	return nil
}

var _ io.Closer = (*terminal.TabStream)(nil)
