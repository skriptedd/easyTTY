# Installation

## Voraussetzungen

| | |
|---|---|
| Betriebssystem | Windows 10 oder 11, 64 Bit |
| WebView2-Runtime | Bei Windows 11 dabei, bei aktuellem Windows 10 meist auch |
| Adminrechte | Nicht nötig |
| Für RDP-Tabs | Remotedesktop-Client von Windows (ist vorinstalliert) |
| Für Serial | Treiber des USB-Konsolenkabels (FTDI, Prolific, CH340 …) |

macOS und Linux werden nicht unterstützt.

## Variante 1: Normale Version

1. Öffne die [Releases](https://github.com/skriptedd/easyTTY/releases).
2. Lade `easyTTY-<version>.exe` herunter.
3. Lege die Datei an einen festen Ort, zum Beispiel `C:\Tools\easyTTY\`.
4. Starte sie per Doppelklick. Beim ersten Start legst du fest, ob und wohin
   Sitzungen protokolliert werden.

Es gibt kein Setup. Die Daten liegen in `%APPDATA%\easyTTY`, Logs und
Firmware-Images in `Dokumente\easyTTY`.

## Variante 2: Portable Version

Für Firmenlaptops ohne Adminrechte oder den USB-Stick:

1. Lade `easyTTY-<version>-portable.zip` herunter.
2. Entpacke den Ordner, wohin du möchtest.
3. Starte `easyTTY.exe`.

Solange `easyTTY.portable` neben der Exe liegt, bleibt alles im Ordner
`easyTTY-data` daneben. Gespeicherte Passwörter sind an dein Windows-Konto
gebunden. Auf einem anderen PC fragt easyTTY sie beim Verbinden neu ab, die
Sessions sind alle da.

Liegt der Ordner in OneDrive, bleiben Sessions und Einstellungen trotzdem dort. Nur der
Zwischenspeicher der Oberfläche (WebView) kommt nach `%LOCALAPPDATA%\easyTTY\portable-webview`,
weil OneDrive dessen offene Datenbanken nicht synchronisiert.

## Download prüfen

Jedes Release enthält `SHA256SUMS.txt`. Prüfen in PowerShell:

```powershell
Get-FileHash .\easyTTY-1.0.0.exe -Algorithm SHA256
```

Der Wert muss mit dem Eintrag in `SHA256SUMS.txt` übereinstimmen. Die Datei selbst ist
signiert (`SHA256SUMS.txt.sig`); die eingebaute Update-Funktion prüft das automatisch.

## Warnungen von Windows

Die Exe ist nicht signiert. Darum kann Windows SmartScreen beim ersten Start
warnen: „Weitere Informationen“ → „Trotzdem ausführen“.

Auch Virenscanner reagieren bei neuen, unsignierten Programmen manchmal mit einem
Fehlalarm. Prüfe dann die Prüfsumme und baue im Zweifel selbst aus dem Quellcode.
Einen Fehlalarm kannst du an den Hersteller melden. Bei Microsoft geht das über
das [Einreichungsportal](https://www.microsoft.com/wdsi/filesubmission).

## Erste Schritte

1. **Sessions übernehmen**: Einstellungen (Zahnrad) → „PuTTY importieren“ oder
   „SecureCRT importieren“.
2. **Oder schnell verbinden**: Oben `benutzer@host` eintippen, Enter. Mit 💾 lässt
   sich die Verbindung später als Session speichern.
3. **Neue Session**: „+ Session“, Protokoll wählen, speichern, Doppelklick zum Verbinden.

### Test ohne echten Server

Aus dem Quellcode lässt sich ein kleiner SSH-Testserver starten:

```powershell
go run ./tools/devsshd
```

Er lauscht auf `127.0.0.1:2222`, Benutzer `test`, Passwort `test`.

## Update

easyTTY sucht beim Start nach einer neuen Version und zeigt unten rechts
„Update … verfügbar“. Ein Klick lädt die neue Exe, prüft die Signatur der
Prüfsummen und die SHA-256-Summe der Exe, sichert deine Daten, ersetzt die Exe und
startet neu. Von Hand geht es in den Einstellungen unter „Updates“. Die Suche beim
Start lässt sich dort abschalten.

Vor jedem Update kopiert easyTTY Sessions, Einstellungen, `known_hosts` und Aufgaben
nach `backups\<Version>_<Datum>` im Datenordner, die letzten fünf bleiben. Die vorige
Exe bleibt als `easyTTY.old.exe` daneben liegen. Die neue Version wird in die
bestehende `easyTTY.exe` geschrieben, die Datei wird nicht umbenannt: Verknüpfungen
und an die Taskleiste angeheftete Symbole bleiben erhalten.

**Zurück zur vorigen Version:** easyTTY schließen, `easyTTY.exe` löschen,
`easyTTY.old.exe` in `easyTTY.exe` umbenennen. Hat die neue Version die Daten schon
geändert, die Dateien aus dem passenden `backups`-Ordner zurück in den Datenordner
kopieren. Ein an die Taskleiste angeheftetes Symbol danach neu anheften.

Liegt die Exe in einem Ordner ohne Schreibrecht, klappt das nicht. Dann die neue
Exe herunterladen und die alte ersetzen. Einstellungen und Sessions bleiben in
beiden Fällen, sie liegen getrennt vom Programm. Das gilt auch für die portable
Version.

Eine neuere Version kann die Sessions-Datei so ändern, dass eine ältere sie nicht
mehr lesen kann. Die ältere Version verweigert dann den Start, damit keine Daten
verloren gehen.

## Daten sichern und umziehen

Sichern: den Ordner `%APPDATA%\easyTTY` kopieren.

Auf einen neuen PC: Einstellungen → Teilen → „Alles exportieren“ und dort
importieren. Passwörter und Benutzernamen sind dabei nicht in der Datei. Kopierst
du stattdessen den ganzen Ordner, sind die Benutzernamen dabei, die Passwörter
lassen sich aber nur unter demselben Windows-Konto lesen.

## Deinstallation

1. easyTTY beenden und die Exe löschen.
2. Daten löschen: `%APPDATA%\easyTTY` und `%APPDATA%\easyTTY.exe`.
3. Bei Bedarf `Dokumente\easyTTY` löschen (Logs, Firmware).

Die Registry wird nicht verändert.

## Fehlerbehebung

| Problem | Lösung |
|---|---|
| SmartScreen: „Der Computer wurde durch Windows geschützt“, Herausgeber unbekannt | Die Exe ist nicht signiert. Prüfsumme mit `SHA256SUMS.txt` vergleichen, dann „Weitere Informationen“ → „Trotzdem ausführen“. Oder vor dem Entpacken: Rechtsklick auf die Zip → Eigenschaften → „Zulassen“ |
| OneDrive: „Der Elementname oder -typ ist nicht zulässig“ für `cache.db` | Ältere portable Version in einem OneDrive-Ordner. Ab 0.2.19 liegt dieser Zwischenspeicher außerhalb von OneDrive; den Ordner `easyTTY-data\webview` darfst du löschen |
| Fenster bleibt leer oder startet nicht | WebView2-Runtime von [Microsoft](https://developer.microsoft.com/microsoft-edge/webview2/) installieren |
| „Hostkey hat sich geändert“ | Nur bestätigen, wenn du den Grund kennst (Neuinstallation, neue IP). Sonst Verbindung abbrechen und den Admin fragen |
| „Der Server bietet keinen Hostkey vom gespeicherten Typ mehr an“ | Server wurde vermutlich neu aufgesetzt. Alten Eintrag aus `known_hosts` im Datenordner entfernen |
| `.ppk`-Schlüssel geht nicht | Mit PuTTYgen ins OpenSSH-Format exportieren (Conversions → Export OpenSSH key) |
| COM-Port fehlt | Treiber des Kabels prüfen, Gerätemanager öffnen, Port-Liste neu laden |
| Serial zeigt Zeichensalat | Baudrate prüfen, Cisco-Konsolen nutzen meist 9600 |
| Firmware-Server wird nicht erreicht | Firewall-Hinweis im Dialog beachten, TFTP braucht UDP 69, Ports unter 1024 sind manchmal belegt |
| RDP bleibt bei „Verbinde…“ | Host und Port prüfen, Remotedesktop auf dem Ziel erlaubt? |
| Alt-Kürzel kollidieren mit bash, tmux oder vim | Einstellungen → Alt-Kürzel abschalten |

## Aus dem Quellcode bauen

Du brauchst [Go](https://go.dev/dl/) 1.27 oder neuer und Git. Node, CGO und die
Wails-CLI sind nicht nötig, die Oberfläche unter `frontend/dist` ist handgeschrieben
und wird nicht gebaut.

```powershell
git clone https://github.com/skriptedd/easyTTY.git
cd easyTTY
.\build.ps1
```

Das Ergebnis liegt in `build\bin\easyTTY.exe`.

| Schalter | Wirkung |
|---|---|
| `-SkipTests` | Tests überspringen |
| `-NoResources` | Ohne Icon und Manifest (sonst lädt das Skript einmalig `go-winres`) |
| `-Portable` | Baut zusätzlich `build\easyTTY-portable.zip` |
| `-Version 1.2.3` | Versionsangabe im Programm |

Ohne das Skript:

```powershell
go run github.com/tc-hib/go-winres@v0.3.3 make --in winres\winres.json
go build -tags desktop,production -trimpath -ldflags "-w -s -H windowsgui" -o easyTTY.exe .
```

Tests:

```powershell
go test ./...
```

Die Tests mit dem echten RDP-Steuerelement brauchen einen Desktop und laufen nur
einzeln:

```powershell
$env:EASYTTY_RDP_SMOKE = "1"
go test ./internal/rdpview -run TestSmokeHostControl
```
