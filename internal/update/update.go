// Package update finds, downloads and installs newer easyTTY releases.
//
// A release is read from the "latest release" endpoint of GitHub or Forgejo
// (same JSON shape). Everything travels over HTTPS from the release host only,
// SHA256SUMS.txt must carry a valid signature (see sign.go), and the
// downloaded exe must match its line in it.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	sumsName     = "SHA256SUMS.txt"
	maxMetaBytes = 1 << 20   // release JSON, checksum file
	maxExeBytes  = 200 << 20 // far above any real build
	maxNotes     = 8000
	maxRedirects = 5
	metaTimeout  = 20 * time.Second
	githubAPI    = "api.github.com"
)

// githubDownloadHosts serve the assets of a GitHub release.
var githubDownloadHosts = []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}

// ErrNoRelease means the source has no usable release (none published, the
// repository is private, or the files are missing).
var ErrNoRelease = errors.New("keine veröffentlichte Version gefunden")

// Release is one published version.
type Release struct {
	Version string // without the leading "v"
	Notes   string
	Page    string // release page for the browser, may be empty

	exeName string
	exeURL  string
	sumsURL string
	sigURL  string
}

// Client talks to one release source.
type Client struct {
	api       string
	apiHost   string
	userAgent string
	key       ed25519.PublicKey // signer of the releases
	HTTP      *http.Client      // exported so tests can swap the transport
}

// New returns a client for an HTTPS "latest release" endpoint whose
// releases are signed with the private half of key.
func New(api, userAgent string, key ed25519.PublicKey) (*Client, error) {
	u, err := url.Parse(api)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("Update-Adresse ungültig: %q", api)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, errors.New("kein Prüfschlüssel für Updates")
	}
	c := &Client{api: api, apiHost: u.Host, userAgent: userAgent, key: key}
	c.HTTP = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("zu viele Weiterleitungen")
		}
		if !c.allowed(req.URL) {
			return fmt.Errorf("Weiterleitung auf fremde Adresse abgelehnt: %s", req.URL.Host)
		}
		return nil
	}}
	return c, nil
}

// allowed: HTTPS on the release host (GitHub hands its files out from a few
// more hosts of its own).
func (c *Client) allowed(u *url.URL) bool {
	if u == nil || u.Scheme != "https" {
		return false
	}
	if u.Host == c.apiHost {
		return true
	}
	return c.apiHost == githubAPI && slices.Contains(githubDownloadHosts, u.Host)
}

func (c *Client) get(ctx context.Context, raw string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil || !c.allowed(u) {
		return nil, fmt.Errorf("Adresse abgelehnt: %q", raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json, application/octet-stream")
	return c.HTTP.Do(req)
}

func (c *Client) getSmall(ctx context.Context, raw string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, metaTimeout)
	defer cancel()
	resp, err := c.get(ctx, raw)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetaBytes+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(body) > maxMetaBytes {
		return nil, resp.StatusCode, errors.New("Antwort zu groß")
	}
	return body, resp.StatusCode, nil
}

type apiRelease struct {
	TagName    string `json:"tag_name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest returns the newest published release.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	body, status, err := c.getSmall(ctx, c.api)
	if err != nil {
		return Release{}, fmt.Errorf("Update-Server nicht erreichbar: %w", err)
	}
	if status == http.StatusNotFound {
		return Release{}, ErrNoRelease
	}
	if status != http.StatusOK {
		return Release{}, fmt.Errorf("Update-Server antwortet mit %d", status)
	}
	var r apiRelease
	if err := json.Unmarshal(body, &r); err != nil {
		return Release{}, fmt.Errorf("Antwort des Update-Servers unlesbar: %w", err)
	}
	version := strings.TrimPrefix(r.TagName, "v")
	if _, ok := parse(version); !ok || r.Draft || r.Prerelease {
		return Release{}, ErrNoRelease
	}
	rel := Release{Version: version, Notes: clip(r.Body, maxNotes), exeName: "easyTTY-" + version + ".exe"}
	if u, err := url.Parse(r.HTMLURL); err == nil && c.allowed(u) {
		rel.Page = r.HTMLURL
	}
	for _, a := range r.Assets {
		switch a.Name {
		case rel.exeName:
			rel.exeURL = a.URL
		case sumsName:
			rel.sumsURL = a.URL
		case SigName:
			rel.sigURL = a.URL
		}
	}
	if rel.exeURL == "" || rel.sumsURL == "" || rel.sigURL == "" { // unsigned releases are not offered
		return Release{}, ErrNoRelease
	}
	return rel, nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// wantedSum finds the SHA-256 of name in a sha256sum listing.
func wantedSum(listing []byte, name string) ([]byte, error) {
	for _, line := range strings.Split(string(listing), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		sum, err := hex.DecodeString(f[0])
		if err != nil || len(sum) != sha256.Size {
			return nil, errors.New("Prüfsumme unlesbar")
		}
		return sum, nil
	}
	return nil, fmt.Errorf("keine Prüfsumme für %s", name)
}

// Download fetches the release exe into dest and checks it against the
// published checksum. On any error nothing is left behind. progress may be nil.
func (c *Client) Download(ctx context.Context, rel Release, dest string, progress func(done, total int64)) error {
	listing, status, err := c.getSmall(ctx, rel.sumsURL)
	if err != nil {
		return fmt.Errorf("Prüfsummen nicht abrufbar: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("Prüfsummen nicht abrufbar: Status %d", status)
	}
	sig, status, err := c.getSmall(ctx, rel.sigURL)
	if err != nil {
		return fmt.Errorf("Signatur nicht abrufbar: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("Signatur nicht abrufbar: Status %d", status)
	}
	if err := verifySig(c.key, listing, sig); err != nil {
		return err
	}
	want, err := wantedSum(listing, rel.exeName)
	if err != nil {
		return err
	}
	resp, err := c.get(ctx, rel.exeURL)
	if err != nil {
		return fmt.Errorf("Download fehlgeschlagen: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Download fehlgeschlagen: Status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxExeBytes {
		return errors.New("Download zu groß")
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "easyTTY-update-*.tmp")
	if err != nil {
		return fmt.Errorf("kein Schreibzugriff neben easyTTY.exe: %w", err)
	}
	keep := false
	defer func() {
		tmp.Close()
		if !keep {
			os.Remove(tmp.Name())
		}
	}()
	hash := sha256.New()
	src := &counter{r: io.LimitReader(resp.Body, maxExeBytes+1), total: resp.ContentLength, report: progress}
	n, err := io.Copy(io.MultiWriter(tmp, hash), src)
	if err != nil {
		return fmt.Errorf("Download abgebrochen: %w", err)
	}
	if n > maxExeBytes {
		return errors.New("Download zu groß")
	}
	if !slices.Equal(hash.Sum(nil), want) {
		return errors.New("Prüfsumme stimmt nicht, Download verworfen")
	}
	if head := make([]byte, 2); !readAt(tmp, head) || string(head) != "MZ" {
		return errors.New("Download ist kein Windows-Programm")
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return err
	}
	keep = true
	return nil
}

func readAt(f *os.File, b []byte) bool {
	_, err := f.ReadAt(b, 0)
	return err == nil
}

// counter reports download progress while reading.
type counter struct {
	r      io.Reader
	done   int64
	total  int64
	report func(done, total int64)
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	if c.report != nil && n > 0 {
		c.report(c.done, c.total)
	}
	return n, err
}
