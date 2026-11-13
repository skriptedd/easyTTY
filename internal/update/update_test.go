package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewerComparesVersions(t *testing.T) {
	cases := []struct {
		current, candidate string
		want               bool
	}{
		{"0.2.13", "0.2.14", true},
		{"0.2.13", "0.3.0", true},
		{"0.9.9", "1.0.0", true},
		{"0.2.13", "0.2.13", false},
		{"0.2.14", "0.2.13", false},
		{"0.2.9", "0.2.10", true},
		{"0.2.13-dev", "0.2.13", true},
		{"0.2.13", "0.2.13-dev", false},
		{"0.2.13", "v0.2.14", true},
		{"0.2.13", "banana", false},
		{"banana", "0.2.14", false},
		{"0.2.13", "0.2", false},
		{"0.2.13", "0.2.-1", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.candidate); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.candidate, got, c.want)
		}
	}
}

// fakeHost serves a release like GitHub and Forgejo do.
type fakeHost struct {
	srv      *httptest.Server
	tag      string
	exe      []byte
	sums     string // "" = computed from exe
	status   int
	draft    bool
	exeURL   string // "" = on this server
	noAssets bool
	noSig    bool               // release without signature file
	signer   ed25519.PrivateKey // who signs SHA256SUMS.txt (the real key unless a test forges)
	pub      ed25519.PublicKey  // what the client trusts
	sig      string             // "" = a real signature, else served as is
}

// sumsText is the checksum file the host serves.
func (f *fakeHost) sumsText() string {
	if f.sums != "" {
		return f.sums
	}
	sum := sha256.Sum256(f.exe)
	return hex.EncodeToString(sum[:]) + "  easyTTY-" + strings.TrimPrefix(f.tag, "v") + ".exe\n" + strings.Repeat("0", 64) + "  other.zip\n"
}

func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeHost{tag: "v0.2.14", exe: []byte("MZ this is the new build"), status: http.StatusOK, signer: priv, pub: pub}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		if f.status != http.StatusOK {
			w.WriteHeader(f.status)
			return
		}
		name := "easyTTY-" + strings.TrimPrefix(f.tag, "v") + ".exe"
		exeURL := f.exeURL
		if exeURL == "" {
			exeURL = f.srv.URL + "/dl/" + name
		}
		assets := []map[string]string{
			{"name": name, "browser_download_url": exeURL},
			{"name": sumsName, "browser_download_url": f.srv.URL + "/dl/" + sumsName},
		}
		if !f.noSig {
			assets = append(assets, map[string]string{"name": SigName, "browser_download_url": f.srv.URL + "/dl/" + SigName})
		}
		if f.noAssets {
			assets = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": f.tag, "body": "- Neues", "html_url": f.srv.URL + "/releases/" + f.tag, "draft": f.draft, "assets": assets,
		})
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, SigName) {
			if f.sig != "" {
				_, _ = w.Write([]byte(f.sig))
			} else {
				_, _ = w.Write(Sign(f.signer, []byte(f.sumsText())))
			}
			return
		}
		if strings.HasSuffix(r.URL.Path, sumsName) {
			_, _ = w.Write([]byte(f.sumsText()))
			return
		}
		_, _ = w.Write(f.exe)
	})
	mux.HandleFunc("/away", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.invalid/easyTTY.exe", http.StatusFound)
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeHost) client(t *testing.T) *Client {
	t.Helper()
	c, err := New(f.srv.URL+"/latest", "easyTTY-test", f.pub)
	if err != nil {
		t.Fatal(err)
	}
	c.HTTP.Transport = f.srv.Client().Transport
	return c
}

func TestLatestReadsRelease(t *testing.T) {
	f := newFakeHost(t)

	rel, err := f.client(t).Latest(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.2.14" || rel.Notes != "- Neues" || !strings.HasSuffix(rel.Page, "/releases/v0.2.14") {
		t.Errorf("unexpected release: %+v", rel)
	}
}

func TestLatestWithoutUsableRelease(t *testing.T) {
	cases := map[string]func(*fakeHost){
		"private or empty repo": func(f *fakeHost) { f.status = http.StatusNotFound },
		"draft":                 func(f *fakeHost) { f.draft = true },
		"odd tag":               func(f *fakeHost) { f.tag = "nightly" },
		"files missing":         func(f *fakeHost) { f.noAssets = true },
		"not signed":            func(f *fakeHost) { f.noSig = true },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeHost(t)
			change(f)

			_, err := f.client(t).Latest(context.Background())

			if !errors.Is(err, ErrNoRelease) {
				t.Errorf("got %v, want ErrNoRelease", err)
			}
		})
	}
}

func TestLatestReportsServerError(t *testing.T) {
	f := newFakeHost(t)
	f.status = http.StatusInternalServerError

	_, err := f.client(t).Latest(context.Background())

	if err == nil || errors.Is(err, ErrNoRelease) {
		t.Errorf("got %v, want a server error", err)
	}
}

func TestNewRefusesPlainHTTPAndMissingKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	for _, api := range []string{"http://example.com/latest", "file:///c:/x", "", "https://"} {
		if _, err := New(api, "t", pub); err == nil {
			t.Errorf("New(%q) accepted", api)
		}
	}
	if _, err := New("https://api.github.com/x", "t", nil); err == nil {
		t.Error("client without public key accepted")
	}
}

func TestSignatureRoundTripAndKeyParsing(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	data := []byte("sums")
	sig := Sign(priv, data)

	if err := verifySig(pub, data, sig); err != nil {
		t.Fatal(err)
	}
	if verifySig(pub, []byte("other"), sig) == nil {
		t.Error("signature valid for other data")
	}
	seed := base64Of(priv.Seed())
	full := base64Of(priv)
	for _, k := range []string{seed, full, " " + seed + "\n"} {
		got, err := ParsePrivateKey(k)
		if err != nil || !got.Equal(priv) {
			t.Errorf("ParsePrivateKey(%q) = %v", k, err)
		}
	}
	if _, err := ParsePrivateKey(base64Of([]byte("short"))); err == nil {
		t.Error("short private key accepted")
	}
	if got, err := ParsePublicKey(base64Of(pub)); err != nil || !got.Equal(pub) {
		t.Errorf("ParsePublicKey: %v", err)
	}
	if _, err := ParsePublicKey("kein base64!"); err == nil {
		t.Error("broken public key accepted")
	}
}

func base64Of(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestDownloadVerifiesChecksum(t *testing.T) {
	f := newFakeHost(t)
	c := f.client(t)
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "new.exe")
	var seen int64

	err = c.Download(context.Background(), rel, dest, func(done, _ int64) { seen = done })

	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(f.exe) || seen != int64(len(f.exe)) {
		t.Errorf("file %q, progress %d", got, seen)
	}
}

func TestDownloadRejectsBadFiles(t *testing.T) {
	cases := map[string]func(*fakeHost){
		"tampered exe":       func(f *fakeHost) { f.sums = strings.Repeat("ab", 32) + "  easyTTY-0.2.14.exe\n" },
		"no checksum line":   func(f *fakeHost) { f.sums = strings.Repeat("ab", 32) + "  something-else.exe\n" },
		"broken checksum":    func(f *fakeHost) { f.sums = "xyz  easyTTY-0.2.14.exe\n" },
		"not a program":      func(f *fakeHost) { f.exe = []byte("<html>login</html>") },
		"foreign host":       func(f *fakeHost) { f.exeURL = "https://example.invalid/easyTTY-0.2.14.exe" },
		"plain http":         func(f *fakeHost) { f.exeURL = strings.Replace(f.srv.URL, "https://", "http://", 1) + "/dl/x.exe" },
		"redirect elsewhere": func(f *fakeHost) { f.exeURL = f.srv.URL + "/away" },
		"forged signature": func(f *fakeHost) {
			_, other, _ := ed25519.GenerateKey(rand.Reader) // someone with upload rights, but not the key
			f.signer = other
		},
		"broken signature": func(f *fakeHost) { f.sig = "das ist keine signatur" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeHost(t)
			change(f)
			c := f.client(t)
			rel, err := c.Latest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()

			err = c.Download(context.Background(), rel, filepath.Join(dir, "new.exe"), nil)

			if err == nil {
				t.Fatal("bad download accepted")
			}
			if left, _ := os.ReadDir(dir); len(left) != 0 {
				t.Errorf("files left behind: %v", left)
			}
		})
	}
}

func TestGithubAssetsMayComeFromGithubHosts(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	c, err := New("https://api.github.com/repos/x/y/releases/latest", "t", pub)
	if err != nil {
		t.Fatal(err)
	}
	for raw, want := range map[string]bool{
		"https://github.com/x/y/releases/download/v1/a.exe": true,
		"https://objects.githubusercontent.com/a":           true,
		"https://github.com.evil.example/a":                 false,
		"http://github.com/a":                               false,
	} {
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		if got := c.allowed(req.URL); got != want {
			t.Errorf("allowed(%s) = %v, want %v", raw, got, want)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
