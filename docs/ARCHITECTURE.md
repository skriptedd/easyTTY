# Architektur

easyTTY ist serviceorientiert aufgebaut: Jede Fachaufgabe ist ein eigener Service
mit klarer Schnittstelle, eigenem Zustand und eigenen Tests. Oben sitzt eine dünne
Schicht, die die Services verdrahtet und der Oberfläche anbietet.

```
┌───────────────────────────────────────────────────────────┐
│ Oberfläche  frontend/dist (HTML, CSS, Vanilla-JS, xterm)  │
└───────────────┬───────────────────────────▲───────────────┘
      Methoden  │ window.go.main.App        │ Ereignisse
┌───────────────▼───────────────────────────┴───────────────┐
│ App  (Wurzelpaket, Fassade für Wails, Dialoge, Verdrahtung)│
└─┬─────┬──────┬──────┬───────┬───────┬───────┬───────┬─────┘
  │     │      │      │       │       │       │       │
store terminal conn transfer rdpview automation fwserver mcpserver
  │                                     │                │
  │                                     └── approval ────┘
fsutil  (Hilfen ohne Fachlogik, von allen nutzbar)
```

Die Regeln:

- Die Abhängigkeiten laufen nur nach unten, es gibt keine Zyklen.
- Ein Service kennt die Oberfläche nicht. Er meldet über Callbacks oder Interfaces
  (`Emitter`, `approval.Notifier`, `mcpserver.Backend`).
- Jeder Service besitzt seinen Zustand, niemand greift von außen hinein.
- Schreiben auf die Platte geht über `fsutil.WriteAtomic`, ein Absturz zerstört
  keine Datei.
- Alles von außen ist unvertrauenswürdig: Serverausgaben, Importdateien,
  Einstellungen und KI-Anfragen werden an der Grenze geprüft.

## Services

| Service | Paket | Aufgabe | Eigener Zustand |
|---|---|---|---|
| Sessions | `internal/store` | Sessions, Ordner, Layouts, Einstellungen, Export/Import, DPAPI | `sessions.json`, `settings.json` |
| Terminal | `internal/terminal` | Ausgabe-Pumpe mit Bündelung, Flusskontrolle, Einfügen, Session-Logs | Offene Verbindungen pro Tab |
| Verbindung | `internal/conn` | SSH, Serial, Hostkeys, Agent, Ziel-Parser | `known_hosts` |
| Dateitransfer | `internal/transfer` | SFTP-Browser, Upload, Download, lokale Dateien | Keiner |
| Remotedesktop | `internal/rdpview`, `internal/rdp` | Eingebettetes RDP-Steuerelement, mstsc-Start | Ansichten auf eigenem Thread |
| Automatisierung | `internal/automation` | Aufgaben, Zeitplan, Befehlsausführung | `tasks.json`, `runs/` |
| Freigabe | `internal/approval` | Entscheidet, was eine KI darf: Rückfragen, 15-Minuten-Freigaben, Lese-Regeln | Offene Fragen, Freigaben |
| KI-Endpunkt | `internal/mcpserver` | MCP über HTTP und stdio-Brücke, Token-Prüfung | Listener |
| Firmware | `internal/fwserver` | Nur-Lese-Server per TFTP, HTTP, SCP/SFTP | Laufender Server |
| Status | `internal/status` | Liest Last, RAM, Platten und Prozesse, Parser für Linux und Cisco | Keiner |
| Importe | `internal/putty`, `internal/securecrt` | Fremde Formate lesen | Keiner |
| Hilfen | `internal/fsutil` | Atomares Schreiben, IDs | Keiner |

`internal/sshtest` ist ein SSH-Testserver für die Tests, `internal/apicontract`
prüft die Schnittstellen gegen [API.md](API.md).

## Wichtige Abläufe

**Verbinden**: Die Oberfläche ruft `Connect`. App baut im Hintergrund die
Verbindung (`conn`), fragt nötige Eingaben über das Ereignis `prompt` ab und hängt
die Verbindung an den `terminal.Manager`. Dieser sendet `term:state` und bündelt
die Ausgabe in `term:data:<tabId>`. Die Oberfläche bestätigt dargestellte Bytes mit
`Ack`, so kann ein schneller Server die Oberfläche nicht überrollen.

**KI-Befehl**: Der MCP-Client ruft `run_commands`. `mcpserver` prüft das Token,
`approval` stellt die Frage in easyTTY. Nach der Freigabe tippt `automation` die
Befehle sichtbar in den Tab.

**RDP**: Das Steuerelement läuft als natives Kind-Fenster auf einem eigenen Thread
mit eigener Nachrichtenschleife. Die Oberfläche meldet die Position des Tabs,
`rdpview` legt das Fenster darüber. Weil native Fenster HTML verdecken, versteckt
es sich, solange ein Dialog oder Menü offen ist.

## Oberfläche

Reines JavaScript ohne Build-Schritt. `frontend/dist` ist Quellcode, trotz des Namens.

| Datei | Aufgabe |
|---|---|
| `js/tab.js`, `js/rdpTab.js` | Ein Terminal- oder RDP-Tab |
| `js/tabs.js` | `TabManager`: Tabs öffnen, anordnen, wechseln |
| `js/tabBlocks.js` | Layout-Blöcke in der Tableiste |
| `js/tree.js` | Session-Baum |
| `js/dialogs.js`, `js/bulkEdit.js`, `js/tasks.js`, `js/firmware.js`, `js/mcp.js` | Dialoge |
| `js/keys.js`, `js/shortcuts.js` | Tastenbelegung und Hilfe |
| `js/api.js`, `js/ui.js` | Zugriff aufs Backend, UI-Bausteine |

Texte kommen nur über `textContent` ins DOM, nie als HTML. Eine
Content-Security-Policy in `index.html` lässt nur eigene Skripte zu.

## Offene Baustellen

Die Service-Grenzen sind noch nicht überall sauber. `App` enthält weiterhin Logik,
die in Services gehört:

| Was | Ziel |
|---|---|
| `app_seriallogin.go`: Konsolen-Login | In `internal/automation` |
| Zweimal Verbindungsaufbau in `app_terminal.go` und `app_automation.go` | Eigener Verbindungs-Service |
| `app_visible.go`: Befehle in sichtbare Tabs tippen | Eigener Service |
| `mcpBackend` in `app_mcp.go` | Eigenes Paket mit schmalen Interfaces |
| SFTP-Client-Pool und Jobs in `app_sftp.go` | `transfer.Service` |
| Zugriff auf `settings.json` ohne Sperre | Einstellungs-Service |
| Ein `a.mu` für viele Felder | Zustand wandert in die Services, die Sperre verschwindet |

Die Oberfläche bleibt dabei unberührt, weil `App` als Fassade alle Methodennamen behält.
