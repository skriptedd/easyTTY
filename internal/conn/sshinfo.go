package conn

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"slices"
	"sync"

	"golang.org/x/crypto/ssh"
)

// SSHInfo describes an established SSH connection: what client and server
// agreed on and which keys were involved. Shown in the tab ("SSH-Verbindung").
type SSHInfo struct {
	Addr          string   `json:"addr"`
	User          string   `json:"user"`
	Jump          []string `json:"jump,omitempty"` // hosts in between, first hop first
	ServerVersion string   `json:"serverVersion"`
	ClientVersion string   `json:"clientVersion"`
	KeyExchange   Algo     `json:"keyExchange"`
	HostKeyAlgo   Algo     `json:"hostKeyAlgo"`
	// To/From are seen from easyTTY: To = client to server.
	CipherTo   Algo    `json:"cipherTo"`
	CipherFrom Algo    `json:"cipherFrom"`
	MACTo      Algo    `json:"macTo"` // empty name: the cipher authenticates itself (GCM, ChaCha20-Poly1305)
	MACFrom    Algo    `json:"macFrom"`
	HostKey    KeyInfo `json:"hostKey"`
	// Auth is the login method that was used last: publickey, password or
	// keyboard-interactive.
	Auth string `json:"auth"`
	// ClientKey is the key file chosen for "Schlüssel" logins; nil when keys
	// came from the agent or the default files.
	ClientKey *KeyInfo `json:"clientKey,omitempty"`
}

// Algo is one negotiated algorithm; Legacy marks those easyTTY only accepts
// for old devices (weak by today's standards).
type Algo struct {
	Name   string `json:"name"`
	Legacy bool   `json:"legacy,omitempty"`
}

// KeyInfo describes a public key.
type KeyInfo struct {
	Type        string `json:"type"`
	Bits        int    `json:"bits,omitempty"`
	Fingerprint string `json:"fingerprint"` // SHA256:…
	Path        string `json:"path,omitempty"`
}

func keyInfo(key ssh.PublicKey) KeyInfo {
	if key == nil {
		return KeyInfo{}
	}
	ki := KeyInfo{Type: key.Type(), Fingerprint: ssh.FingerprintSHA256(key)}
	if ck, ok := key.(ssh.CryptoPublicKey); ok {
		switch k := ck.CryptoPublicKey().(type) {
		case *rsa.PublicKey:
			ki.Bits = k.N.BitLen()
		case *ecdsa.PublicKey:
			ki.Bits = k.Curve.Params().BitSize
		}
	}
	return ki
}

func algo(name string, legacy []string) Algo {
	return Algo{Name: name, Legacy: name != "" && slices.Contains(legacy, name)}
}

// fillAlgorithms copies what the handshake agreed on into info.
func fillAlgorithms(info *SSHInfo, sc ssh.Conn) {
	info.ServerVersion = string(sc.ServerVersion())
	info.ClientVersion = string(sc.ClientVersion())
	am, ok := sc.(ssh.AlgorithmsConnMetadata)
	if !ok {
		return
	}
	a := am.Algorithms()
	weak := ssh.InsecureAlgorithms()
	info.KeyExchange = algo(a.KeyExchange, weak.KeyExchanges)
	info.HostKeyAlgo = algo(a.HostKey, weak.HostKeys)
	info.CipherTo = algo(a.Write.Cipher, weak.Ciphers)
	info.CipherFrom = algo(a.Read.Cipher, weak.Ciphers)
	info.MACTo = algo(a.Write.MAC, weak.MACs)
	info.MACFrom = algo(a.Read.MAC, weak.MACs)
}

// authTrace notes which login steps ran during one handshake.
type authTrace struct {
	mu        sync.Mutex
	method    string
	clientKey *KeyInfo
}

func (t *authTrace) tried(method string) {
	t.mu.Lock()
	t.method = method
	t.mu.Unlock()
}

func (t *authTrace) keyFile(path string, key ssh.PublicKey) {
	ki := keyInfo(key)
	ki.Path = path
	t.mu.Lock()
	t.clientKey = &ki
	t.mu.Unlock()
}

func (t *authTrace) fill(info *SSHInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	info.Auth = t.method
	if t.method == "publickey" {
		info.ClientKey = t.clientKey
	}
}
