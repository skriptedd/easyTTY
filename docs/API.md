# Schnittstellen

Alle Schnittstellen von easyTTY: Backend-Methoden, Ereignisse, MCP-Werkzeuge,
Netzwerkdienste, Dateien und Kommandozeile. Ein Test
(`internal/apicontract`) prüft, dass jede Methode, jedes Ereignis und jedes
MCP-Werkzeug hier steht.

## Grundregeln

- Die Oberfläche ruft `api.<Methode>(…)` auf, das ist ein Proxy über
  `window.go.main.App`. Jeder Aufruf liefert ein Promise.
- Ein Go-Ergebnis `(T, error)` löst mit `T` auf oder lehnt mit dem deutschen
  Fehlertext ab.
- Nil-Zeiger und Nil-Slices kommen als `null` an.
- Tab-IDs vergibt die Oberfläche (`t…`) oder das Backend (`k` + 16 Hex-Zeichen).
- Statuswerte wie `läuft` oder `fehler` sind deutsch und werden so gespeichert.

## Backend-Methoden

### Terminal und Tabs

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `Connect` | `tabID, sessionID: string, cols, rows: int` | `error` | Ergebnis kommt als `term:state` |
| `QuickConnect` | `tabID, target: string, cols, rows: int` | `conn.Target, error` | Liest `user@host:port`, speichert nichts |
| `CloseTab` | `tabID: string` | – | Bricht Verbindungsaufbau und Einfügen ab |
| `Write` | `tabID, data: string` | `error` | Tastatureingabe |
| `Resize` | `tabID: string, cols, rows: int` | `error` | |
| `Ack` | `tabID: string, n: int` | – | Flusskontrolle: dargestellte Bytes |
| `SendBreak` | `tabID: string` | `error` | Serial-Break, Tab-Menü „Break senden“ |
| `Paste` | `tabID, text: string, lineDelayMs: int` | `error` | `<0` = Einstellung, max. 2000; Fortschritt per `paste:progress` |
| `CancelPaste` | `tabID: string` | – | |
| `SerialLogin` | `tabID: string` | `error` | Nur gespeicherte Serial-Sessions |
| `TrackTab` | `tabID, title, sessionID: string` | – | Meldet einen Tab für MCP an |
| `CancelAI` | `tabID: string` | `bool` | Stoppt Automatik in einem Tab |
| `AnswerPrompt` | `id: string, ok: bool, values: string[]` | – | Antwort auf `prompt` |
| `RevealLog` | `path: string` | `error` | Explorer markiert die Datei |
| `OpenLogDir` | – | `error` | Legt den Ordner bei Bedarf an |

### Sessions und Ordner

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `GetTree` | – | `store.Tree` | Enthält nie Geheimnisse |
| `SaveSession` | `s: store.Session, password: *string` | `store.Session, error` | `null` = unverändert, `""` = entfernen, sonst verschlüsselt speichern |
| `DeleteSession` | `id: string` | `error` | |
| `DeleteSessions` | `ids: string[]` | `error` | Alles oder nichts |
| `DuplicateSession` | `id: string` | `store.Session, error` | Ohne Passwort |
| `MoveSession` | `id, folderID: string` | `error` | `""` = oberste Ebene |
| `PatchSessions` | `folderID: string, recursive: bool, p: store.SessionPatch` | `int, error` | Anzahl geänderter Sessions, alles oder nichts |
| `FormatSessionName` | `name: string` | `string` | Vorschau Namensschema |
| `ApplyNamingToAll` | – | `int, error` | Anzahl umbenannter Sessions |
| `SaveFolder` | `f: store.Folder` | `store.Folder, error` | Anlegen oder umbenennen |
| `SaveFolderLayout` | `id: string, l: *store.Layout` | `store.Folder, error` | `null` entfernt das Layout |
| `MoveFolder` | `id, parentID: string` | `error` | |
| `DeleteFolder` | `id: string, withSessions: bool` | `error` | `false` schiebt den Inhalt eine Ebene hoch |
| `ListSerialPorts` | – | `conn.SerialPortInfo[], error` | |
| `PickKeyFile` | – | `string, error` | Dateidialog |
| `PickLogFile` | – | `string, error` | Dateidialog |
| `PickLogDir` | `current: string` | `string, error` | Ordnerdialog |

### Einstellungen und Programm

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `GetSettings` | – | `store.Settings` | `logDir` aufgelöst, enthält `mcpToken` |
| `SaveSettings` | `s: store.Settings` | `store.Settings, error` | Leerer `mcpToken` behält den alten; wendet MCP- und RDP-Einstellungen an |
| `AppInfo` | – | `{name, version, dataDir, mode}` | `mode`: `installiert` oder `portabel` |
| `OpenDataDir` | – | `error` | |
| `CheckUpdate` | – | `{current, version, available, notes, page}, error` | Fragt die Release-Seite; ohne veröffentlichte Version bleibt `version` leer |
| `InstallUpdate` | `version: string` | `error` | Lädt, prüft die SHA-256-Summe, tauscht die Exe und startet neu |

### Import und Teilen

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `ImportPutty` | – | `ImportResult, error` | Ordner „PuTTY Import“ |
| `ImportSecureCRT` | `from: string` | `SecureCRTResult, error` | `""` sucht selbst, `"dir"`, `"xml"` |
| `ExportShare` | `folderIDs, sessionIDs: string[], all, withSettings: bool` | `string, error` | Pfad, `""` bei Abbruch |
| `PickShare` | – | `*SharePreview, error` | `null` bei Abbruch |
| `ApplyShare` | `path: string, withSettings: bool` | `store.ImportSummary, error` | Log-, KI- und Sicherheitseinstellungen bleiben lokal |

### SFTP und lokale Dateien

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `SftpList` | `tabID, dir: string` | `transfer.Listing, error` | `""` = Home |
| `SftpMkdir` | `tabID, dir, name: string` | `error` | |
| `SftpRename` | `tabID, from, to: string` | `error` | |
| `SftpRemove` | `tabID: string, paths: string[]` | `error` | Rekursiv |
| `SftpUpload` | `tabID, remoteDir: string, localPaths: string[], onExist: string` | `string, error` | Job-ID, `""` wenn nichts zu tun ist. `onExist`: `overwrite`, `rename` (Name (1)) oder `skip` für vorhandene Namen. Ersetzt Zieldateien erst nach vollständiger Übertragung |
| `SftpDownload` | `tabID: string, remotePaths: string[]` | `string, error` | Zeigt Ordnerdialog |
| `SftpDownloadTo` | `tabID: string, remotePaths: string[], localDir: string` | `string, error` | Überschreibt nie lokale Dateien |
| `CancelTransfer` | `jobID: string` | – | |
| `PickUploadFiles` | – | `string[], error` | |
| `PickUploadFolder` | – | `string[], error` | Null oder ein Eintrag |
| `LocalList` | `dir: string` | `transfer.Listing, error` | `""` = Laufwerke |
| `LocalParent` | `dir: string` | `string` | `""` über einem Laufwerk |
| `LocalHome` | – | `string` | Letzter Download-Ordner |
| `RevealLocal` | `path: string` | `error` | |

### RDP

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `ConnectRDP` | `id: string` | `error` | Externes mstsc-Fenster |
| `RDPOpen` | `tabID, sessionID: string, r: rdpview.Rect` | `error` | Physische Pixel |
| `RDPMove` | `tabID: string, r: rdpview.Rect, visible: bool` | `error` | Ohne laufenden Host wirkungslos |
| `RDPFit` | `tabID: string` | `error` | Auflösung an den Tab anpassen |
| `RDPFocus` | `tabID: string` | `error` | |
| `RDPClose` | `tabID: string` | `error` | |

### Server-Status

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `ServerStatus` | `tabID: string` | `status.Snapshot, error` | Nur SSH-Tabs |

### Aufgaben

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `TaskOverview` | – | `TaskView[], error` | |
| `SaveTask` | `t: automation.Task` | `automation.Task, error` | Leere `id` legt an |
| `DeleteTask` | `id: string` | `error` | Verlauf bleibt |
| `RunTaskNow` | `id: string` | `error` | Läuft sichtbar im Tab, Fortschritt per `task:run` |
| `CancelTask` | `id: string` | `bool` | |
| `TaskRuns` | `taskID: string, limit: int` | `automation.Run[], error` | `""` = alle, `0` = 50, ohne Ausgabe |
| `TaskRun` | `id: string` | `automation.Run, error` | Mit voller Ausgabe |

### Firmware-Server

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `FirmwareState` | – | `FirmwareState` | |
| `FirmwareStart` | `cfg: fwserver.Config` | `fwserver.Info, error` | Erzeugt ein neues Passwort |
| `FirmwareStop` | – | – | |
| `FirmwareFiles` | `dir: string` | `fwserver.File[], error` | `""` = Standardordner, max. 500 |
| `PickFirmwareDir` | `current: string` | `string, error` | |
| `OpenFirmwareDir` | `dir: string` | `error` | |
| `FirewallStatus` | – | `FirewallInfo` | Liest nur, ändert keine Regeln |

### Running-Configs sichern

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `ConfigDir` | – | `string, error` | Config-Ordner (Einstellung `configDir`, sonst `Dokumente\easyTTY\Configs`), wird angelegt |
| `PickConfigDir` | – | `string, error` | Ordnerdialog, speichert die Wahl in den Einstellungen |
| `OpenConfigDir` | – | `error` | Explorer |
| `RunSnippetToFile` | `tabID, name, text: string` | `string, error` | Führt die Befehle im offenen Tab nacheinander aus und speichert Befehl und Ausgabe als `<Tab>_<Name>_<Datum>_<Zeit>.txt` im Config-Ordner. Ergebnis: Pfad |
| `ConfigFiles` | – | `ConfigFile[], error` | Dateien im Config-Ordner, neueste zuerst, höchstens 500: `{name, path, size, modTime, device}` |
| `DiffConfigs` | `oldPath, newPath: string, ignoreVolatile: bool` | `{lines[{kind (" "/"-"/"+"), text, oldNo?, newNo?}], removed, added}, error` | Zeilenvergleich zweier Dateien im Config-Ordner (je höchstens 5 MB). `ignoreVolatile` lässt Zeitstempel-Zeilen weg |
| `DefaultHighlights` | – | `store.Highlight[]` | Standard-Hervorhebungen (IP, MAC, Fehler, OK) |
| `OpenConfigFile` | `path: string` | `error` | Öffnet eine gesicherte Config mit dem Standardprogramm; nur Dateien im Config-Ordner |
| `SaveRunningConfig` | `tabID: string` | `string, error` | Tippt `show running-config` in den offenen Tab (SSH oder Seriell), blättert durch `--More--`, speichert als `<Tab>_<Datum>_<Zeit>.cfg`. Ergebnis: Pfad |

### KI-Zugriff

| Methode | Argumente | Ergebnis | Hinweis |
|---|---|---|---|
| `AnswerApproval` | `id, answer: string` | – | `once`, `grant`, sonst Ablehnung |
| `GetMcpInfo` | – | `McpInfo` | |
| `NewMcpToken` | – | `McpInfo, error` | Alte Tokens werden ungültig |

### Datentypen

| Typ | Felder |
|---|---|
| `store.Session` | `id, name, folderId, protocol (ssh/serial/rdp), host, port, user, auth (auto/password/key/agent), keyPath, hasPassword, serialPort, baud, dataBits, parity (none/odd/even/mark/space), stopBits (1/1.5/2), keepAlive, jumpId, forwards[{listen, host, port}], rdpDrives, rdpNoClipboard, logPath, color, notes, lastUsed` |
| `store.Folder` | `id, name, parentId, layout?` |
| `store.Layout` | `template (1/2v/2h/3m/4/6), slots: string[]` |
| `store.Tree` | `folders, sessions` |
| `store.SessionPatch` | Alle optional: `user, auth, keyPath, keepAlive, color, password` |
| `store.ImportSummary` | `folders, sessions, skipped, settingsApplied` |
| `ImportResult` | `imported, skipped` |
| `SecureCRTResult` | `notFound, source, imported, existing, skipped: string[]` |
| `SharePreview` | `path, folders, sessions, hasSettings, exported` |
| `conn.Target` | `user, host, port` |
| `conn.SerialPortInfo` | `name, description, vendor, usb, console` |
| `transfer.Listing` | `path, entries: Entry[]` |
| `transfer.Entry` | `name, path, size, mode, modTime, isDir, isLink` |
| `rdpview.Rect` | `x, y, w, h` |
| `status.Snapshot` | `kind (linux/cisco), host, os, uptimeSec, uptime, load[], cpus, cpu[], memTotal, memUsed, swapTotal, swapUsed, cpuUsage, disks[{mount,total,used}], procs[{pid,name,cpu,mem}], taken` |
| `automation.Task` | `id, name, sessionIds[], commands, schedule, enabled, device (auto/cisco/other), stopOnError, autoConfirm, idleTimeout, createdBy?, modifiedBy?, created, updated` |
| `automation.Schedule` | `kind (manual/once/daily/weekly/interval), date?, at?, days?, everyMin?` |
| `automation.Run` | `id, taskId?, taskName, trigger, start, end, state (läuft/ok/fehler/abgebrochen/verpasst), error?, sessions: SessionRun[]` |
| `automation.SessionRun` | `sessionId, session, result, logPath?` |
| `automation.Result` | `prompt, steps: Step[], error?` |
| `automation.Step` | `command, output, error?, ms` |
| `TaskView` | `task, when, next (Unix-ms, 0 = keiner), running, lastRun?, sessions: string[]` |
| `fwserver.Config` | `dir, bindIp, tftpPort, httpPort, sshPort` (`0` = aus), `uploadDir` (`""` = nur lesen; muss außerhalb von `dir` liegen) |
| `fwserver.Info` | `running, config, user, password, started` |
| `fwserver.File` | `name, size, modTime, md5` |
| `fwserver.Address` | `ip, interface` |
| `FirmwareState` | `info, defaultDir, addresses[], events[]` |
| `FirewallInfo` | `checked, level (ok/warn/unknown), message` |
| `McpInfo` | `enabled, running, url, token, error?, exe` |

### Einstellungen (`settings.json`)

| Feld | Standard | Regel |
|---|---|---|
| `fontFamily` | `Cascadia Mono, Consolas, monospace` | |
| `fontSize` | `14` | 8 bis 32 |
| `theme` | `dark` | `dark`, `light` |
| `scrollback` | `20000` | 0 bis 1.000.000 |
| `copyOnSelect`, `rightClickPaste`, `cursorBlink`, `confirmClose` | `true` | |
| `logEnabled` | `true` | |
| `logDir` | `""` | `""` = Standardordner |
| `logRaw`, `firstRunDone` | `false` | |
| `pasteConfirm` | `true` | |
| `pasteLineDelay` | `0` | 0 bis 2000 ms |
| `nameCase` | `""` | `lower`, `upper`, `capitalize`, `title` |
| `nameSpaces` | `""` | `_`, `-` |
| `serialWake`, `altShortcuts` | `true` | |
| `mcpEnabled`, `mcpShowAuto`, `mcpReadTabs` | `false` | |
| `mcpPort` | `47321` | 1024 bis 65535 |
| `mcpToken` | `""` | Wird beim ersten Einschalten erzeugt, liegt im Klartext |
| `typeSpeed` | `""` | `fast`, `slow` |
| `updateCheck` | `true` | Beim Start nach einer neuen Version suchen |
| `highlights` | IP, MAC, Fehler, OK | `[{name, kind (words/regex), pattern, color (red/green/yellow/blue/magenta/cyan), off}]`, höchstens 50; färbt nur die Anzeige |
| `snippets` | `null` | `[{name, text, saveOutput?}]`, höchstens 200, Befehls-Schnipsel (Alt+J) |

## Ereignisse (Backend an Oberfläche)

| Ereignis | Nutzdaten |
|---|---|
| `term:state` | `{id, state, message}`; `connecting`, `connected`, `closed`, `error` |
| `term:data:<tabId>` | Base64-String mit Rohdaten |
| `term:log` | `{id, path}` |
| `notify` | `{kind, message}`; `ok`, `err`, `info` |
| `paste:progress` | `{id, state (running/done/cancelled/error), done, total, timeouts, error?, stopLine?, stopText?, misses?}` |
| `sftp:progress` | `{jobId, tabId, kind (upload/download), state (running/done/error/cancelled), file, done, total, files, filesDone, skipped, dest?, error?}` |
| `prompt` | `{id, tabId, kind (hostkey/username/password/passphrase/challenge), title, message, fields[{label,secret,value}], hostKey?}`; `hostKey`: `{host, keyType, fingerprint, changed, oldKeyType?, oldFingerprint?, oldSource?}` |
| `prompt:cancel` | Prompt-ID (Timeout nach 5 Minuten oder Abbruch) |
| `sessions:changed` | `null` |
| `rdp:state` | `{id, state (connecting/connected/closed), message}` |
| `update:available` | `{current, version, available, notes, page}`; einmal kurz nach dem Start, wenn es eine neuere Version gibt |
| `update:progress` | Prozent des Downloads (0–100) |
| `rdp:key` | `{id, state: "key", message}`; `w e f s k t 0-9 ArrowLeft ArrowUp ArrowRight ArrowDown` |
| `fw:event` | `{time, proto, client, file, state (läuft/fertig/abgebrochen/fehler/abgelehnt/info), done, total, detail, md5?, upload?}`; `upload`: das Gerät hat die Datei geschickt, `detail` nennt dann den gespeicherten Namen |
| `task:run` | `automation.Run`, meist ohne Befehlsausgabe |
| `mcp:approve` | `{id, client, action, target, detail, grantable, timeout}` |
| `mcp:approve:cancel` | Anfrage-ID |
| `mcp:approve:done` | Anfrage-ID |
| `tab:open` | `{tabId, sessionId}` |
| `tab:focus` | Tab-ID |
| `tab:close` | Tab-ID |
| `tab:ai` | `{tabId, client, active}` |

## MCP-Werkzeuge

Server-Name `easyTTY`. Die Brücke `easyTTY.exe --mcp` bietet dieselben Werkzeuge
über stdio an und gibt den Client-Namen weiter. Der Name dient nur der Anzeige.

| Werkzeug | Parameter | Ergebnis | Freigabe und Grenzen |
|---|---|---|---|
| `list_sessions` | – | `{sessions: [{id, name, folder, protocol, target, hasCredentials}]}` | Keine. Ohne RDP, ohne Geheimnisse |
| `list_tabs` | – | `{tabs: [{tabId, title, sessionId?, folder?}]}` | Keine. Nur verbundene Tabs |
| `read_tab` | `tabId`, `lines?` (Standard 60, max. 2000) | `{text}` | Nur wenn „Tabs lesen“ an ist |
| `send_to_tab` | `tabId`, `text`, `pressEnter?` | `{ok}` | Freigabe, 15 Minuten möglich. Unsichtbare Zeichen werden abgelehnt |
| `run_commands` | `sessions[]` (IDs oder Namen), `commands` (je Zeile ein Befehl, `!` und `#` werden übersprungen), `device?` (auto/cisco/other), `stopOnError?` (Standard an) | `automation.Run` mit Ausgabe | Freigabe, 15 Minuten möglich. Entfällt nur bei `mcpShowAuto` und reinen show-/display-Befehlen. Max. 20 Geräte, 200 Befehle, 2 Läufe gleichzeitig. Läuft sichtbar im Tab, `[confirm]`-Rückfragen werden bestätigt |
| `list_tasks` | – | `{tasks: [{task, when, nextRun?, lastState?}]}` | Keine |
| `save_task` | `id?`, `name`, `sessions[]`, `commands`, `schedule`, `enabled?`, `device?`, `stopOnError?`, `autoConfirm?` | `automation.Task` | Freigabe jedes Mal. Eine Änderung behält `idleTimeout` |
| `delete_task` | `id` | `{ok}` | Freigabe jedes Mal |
| `run_task` | `id` | `automation.Run` | Freigabe jedes Mal. Abgelehnt, wenn die Aufgabe währenddessen geändert wurde |
| `list_runs` | `taskId?`, `limit?` (Standard 20, 1 bis 200) | `{runs: Run[]}` ohne Ausgabe | Keine |
| `get_run` | `runId` | `automation.Run` | Keine. Ausgabe nur, wenn „Tabs lesen“ an ist |

Freigaben gelten für genau diesen Client und genau diesen Inhalt. Alles hängt am
Paket `internal/approval`.

## Netzwerkdienste

| Dienst | Adresse | Anmeldung | Verhalten |
|---|---|---|---|
| MCP `/mcp` | `127.0.0.1:<mcpPort>`, Standard 47321 | `Authorization: Bearer <token>` | Streamable HTTP. Falsches Token: 401. Andere Rechner kommen nicht heran |
| Firmware HTTP | `<bindIp>:<httpPort>`, Vorgabe 8080 | Keine | `GET`/`HEAD`, Dateiliste mit MD5. Nur lesend, andere Methoden: 405 |
| Firmware TFTP | UDP `<bindIp>:<tftpPort>`, Vorgabe 69 | Keine | Lesen; Schreiben nur mit `uploadDir`, dann nur dorthin, nie überschreibend, höchstens 64 MB. `blksize` (höchstens 8192), `tsize`, `timeout`; 16 Übertragungen gesamt, 2 pro Gerät, 30 Anfragen pro Gerät in 10 Sekunden |
| Port-Weiterleitung | TCP `127.0.0.1:<listen>` je Eintrag in `forwards` | Keine (nur dieser PC) | Nur solange die SSH-Session verbunden ist; tunnelt zu `host:port` aus Sicht des Servers |
| Firmware SCP/SFTP | TCP `<bindIp>:<sshPort>`, Vorgabe 22 | Benutzer `firmware`, Zufallspasswort pro Start | Nur `scp -f` und lesendes SFTP; neue Hostkeys pro Start; 32 Verbindungen |

Der Firmware-Server läuft nur, wenn du ihn startest. Ohne gewählte Adresse lauscht
er auf allen Netzwerkkarten. Ein ganzes Laufwerk lässt sich nicht freigeben.

## Dateien

Datenordner: installiert `%APPDATA%\easyTTY\`, portabel `<Exe-Ordner>\easyTTY-data\`.

| Datei | Ort | Inhalt |
|---|---|---|
| `sessions.json` | Datenordner | JSON `{version: 1, folders, sessions, secrets}`. `secrets`: Session-ID auf Base64-DPAPI-Daten |
| `sessions.json.bak`, `.damaged` | Datenordner | Letzter guter Stand; beschädigte Datei nach Rücksprung auf `.bak` |
| `settings.json` (+ `.bak`, `.damaged`) | Datenordner | `store.Settings`, `mcpToken` im Klartext |
| `known_hosts` (+ `.bak`) | Datenordner | OpenSSH-Format |
| `crash.log` (+ `.old`) | Datenordner | Startzeilen und Stacktrace bei Abstürzen, bleibt lokal |
| `automation/tasks.json` (+ `.bak`, `.damaged`) | Datenordner | Array aus `automation.Task` |
| `automation/runs/<runId>.json` | Datenordner | `automation.Run` mit Ausgabe. 50 pro Aufgabe, 200 ohne Aufgabe |
| `Logs\{date}\{session}_{time}.log` | `<Dokumente>\easyTTY\Logs`, portabel `<Daten>\Logs` | `[Zeitstempel] Zeile`, Kopf und Fuß. Platzhalter: `{session} {host} {date} {time}` |
| Firmware-Images | `<Dokumente>\easyTTY\Firmware`, portabel `<Daten>\Firmware` | Werden nur gelesen |
| WebView-Profil | `%APPDATA%\easyTTY.exe\`, portabel `<Daten>\webview\` | Oberflächenzustand (`localStorage`) |
| `easyTTY.portable` | Neben der Exe | Merker für den portablen Betrieb, nur die Existenz zählt |
| `*.easytty.json` | Frei wählbar | `{format: "easytty-share", version: 1, exported, folders, sessions, settings?}`. Ohne Benutzer, Passwörter, Schlüssel- und Log-Pfade. Max. 20 MiB |
| `<16 Hex>.rdp` | `%TEMP%\easyTTY-rdp\` | Ohne Passwort, wird nach dem Ende von mstsc gelöscht |
| `<Datei>.*.part` | Zielordner beim Download | Temporär |
| `<Datei>.easytty-part` | Zielordner auf dem Server beim Upload | Temporär |
| `~\.ssh\known_hosts`, `~\.ssh\id_ed25519`, `id_ecdsa`, `id_rsa` | Benutzerordner | Nur lesend |
| `HKCU\Software\SimonTatham\PuTTY\Sessions`, `HKCU\Software\VanDyke\SecureCRT`, `%APPDATA%\VanDyke\Config\Sessions` | Registry, Dateien | Importquellen, nur lesend |

## Kommandozeile und Umgebung

| Eintrag | Wert | Bedeutung |
|---|---|---|
| `easyTTY.exe` | – | Startet das Programm |
| `easyTTY.exe --mcp` | Erstes Argument | stdio-Brücke zu einem laufenden easyTTY |
| `EASYTTY_MCP_URL` | Umgebung, nur Brücke | Standard `http://127.0.0.1:<mcpPort>/mcp`, nur Loopback |
| `EASYTTY_MCP_TOKEN` | Umgebung, nur Brücke | Standard aus `settings.json` |
| `SSH_AUTH_SOCK` | Umgebung | Agent-Socket oder Named Pipe |
| `EASYTTY_RDP_SMOKE` | Umgebung, nur Tests | Schaltet RDP-Tests mit echtem Steuerelement ein |
| `-X main.appVersion=<v>` | Linker | Versionsangabe |
| `-tags desktop,production` | Build | Release-Build |

Unbekannte Argumente werden ignoriert.
