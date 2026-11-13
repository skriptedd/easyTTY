package conn

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"easytty/internal/fsutil"
)

// HostKeyPrompt describes an unknown or changed host key for the user.
type HostKeyPrompt struct {
	Host        string `json:"host"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
	Changed     bool   `json:"changed"`
	// For a changed key: the stored one and where it is (file, line).
	OldKeyType     string `json:"oldKeyType,omitempty"`
	OldFingerprint string `json:"oldFingerprint,omitempty"`
	OldSource      string `json:"oldSource,omitempty"`
}

// HostKeyConfirm asks the user whether to trust a key.
type HostKeyConfirm func(HostKeyPrompt) bool

// KnownHosts verifies host keys against the app's known_hosts file (writable)
// and optional read-only files such as ~/.ssh/known_hosts (trust-on-first-use).
type KnownHosts struct {
	mu       sync.Mutex
	appFile  string
	readOnly []string
	confirm  HostKeyConfirm
}

// NewKnownHosts creates a verifier. appFile is created on first accepted key.
func NewKnownHosts(appFile string, readOnly []string, confirm HostKeyConfirm) *KnownHosts {
	return &KnownHosts{appFile: appFile, readOnly: readOnly, confirm: confirm}
}

// Callback returns an ssh.HostKeyCallback using the default confirm function.
func (k *KnownHosts) Callback() ssh.HostKeyCallback { return k.CallbackWith(k.confirm) }

// CallbackWith returns an ssh.HostKeyCallback that asks confirm for unknown keys
// (lets each connection bind the prompt to its own tab and cancellation).
func (k *KnownHosts) CallbackWith(confirm HostKeyConfirm) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := k.check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		// Any known key counts: HostKeyAlgorithms restricts the server to the
		// stored key types, so a different type here means a downgrade/MITM.
		changed := len(ke.Want) > 0
		prompt := HostKeyPrompt{
			Host:        knownhosts.Normalize(hostname),
			KeyType:     key.Type(),
			Fingerprint: ssh.FingerprintSHA256(key),
			Changed:     changed,
		}
		if changed { // show what was stored and where, so a reinstall can be told from an attack
			old := ke.Want[0]
			for _, w := range ke.Want {
				if w.Key.Type() == key.Type() {
					old = w
					break
				}
			}
			prompt.OldKeyType, prompt.OldFingerprint = old.Key.Type(), ssh.FingerprintSHA256(old.Key)
			prompt.OldSource = fmt.Sprintf("%s, Zeile %d", old.Filename, old.Line)
		}
		if confirm == nil || !confirm(prompt) {
			return errors.New("Hostkey nicht akzeptiert")
		}
		return k.add(hostname, key, changed)
	}
}

// HostKeyAlgorithms returns the algorithms of keys stored for addr ("host:port"),
// so the client only accepts those types. nil means the host is unknown.
func (k *KnownHosts) HostKeyAlgorithms(addr string) []string {
	err := k.check(addr, &net.TCPAddr{}, probeKey)
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) || len(ke.Want) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var algos []string
	for _, w := range ke.Want {
		for _, a := range algorithmsFor(w.Key.Type()) {
			if !seen[a] {
				seen[a] = true
				algos = append(algos, a)
			}
		}
	}
	return algos
}

func algorithmsFor(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

// probeKey never matches a real host; it only makes knownhosts list the stored keys.
var probeKey = func() ssh.PublicKey {
	k, err := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	if err != nil {
		panic(err)
	}
	return k
}()

func (k *KnownHosts) check(hostname string, remote net.Addr, key ssh.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	files := existing(append([]string{k.appFile}, k.readOnly...))
	if len(files) == 0 {
		return &knownhosts.KeyError{}
	}
	cb, err := knownhosts.New(files...)
	if err != nil {
		// A foreign known_hosts we cannot parse must not lock the user out.
		files = existing([]string{k.appFile})
		if len(files) == 0 {
			return &knownhosts.KeyError{}
		}
		if cb, err = knownhosts.New(files...); err != nil {
			return fmt.Errorf("known_hosts lesen: %w", err)
		}
	}
	return cb(hostname, remote, key)
}

// add stores key for hostname; replace drops old keys of the same type first.
func (k *KnownHosts) add(hostname string, key ssh.PublicKey, replace bool) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	host := knownhosts.Normalize(hostname)
	var keep []string
	raw, err := os.ReadFile(k.appFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Never rewrite the file from scratch when it just can't be read
		// right now (virus scanner, sync tool): that would drop every host.
		return fmt.Errorf("known_hosts lesen: %w", err)
	}
	if err == nil {
		for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if replace && lineMatches(line, host, key.Type()) {
				continue
			}
			keep = append(keep, line)
		}
	}
	keep = append(keep, knownhosts.Line([]string{host}, key))
	return fsutil.WriteAtomic(k.appFile, []byte(strings.Join(keep, "\n")+"\n"))
}

func lineMatches(line, host, keyType string) bool {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[1] != keyType {
		return false
	}
	for _, h := range strings.Split(fields[0], ",") {
		if h == host {
			return true
		}
	}
	return false
}

func existing(paths []string) []string {
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}
