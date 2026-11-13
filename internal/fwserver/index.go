package fwserver

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
)

// serveIndex lists the served files, so a browser on "http://IP:port/"
// shows what a device can fetch (and proves the server is reachable).
func serveIndex(w http.ResponseWriter, root string) {
	files, err := ListFiles(root)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>easyTTY Firmware-Server</title>` +
		`<style>body{font:14px system-ui,sans-serif;margin:24px;color:#222}td{padding:3px 14px 3px 0}code{color:#666}</style>` +
		`<h1>easyTTY Firmware-Server</h1><p>Nur lesend. Auf dem Switch z. B.: <code>copy http://&lt;IP&gt;:&lt;Port&gt;/&lt;Datei&gt; flash:</code></p><table>`)
	if err != nil {
		b.WriteString("<tr><td>Ordner nicht lesbar</td></tr>")
	}
	for _, f := range files {
		fmt.Fprintf(&b, `<tr><td><a href="/%s">%s</a></td><td>%.1f MB</td><td><code>%s</code></td></tr>`,
			html.EscapeString((&url.URL{Path: f.Name}).EscapedPath()), html.EscapeString(f.Name), float64(f.Size)/(1<<20), f.MD5)
	}
	if len(files) == 0 && err == nil {
		b.WriteString("<tr><td>Keine Dateien im Ordner.</td></tr>")
	}
	b.WriteString("</table>")
	_, _ = w.Write([]byte(b.String()))
}
