# easyTTY

### *Where Consoles Connect.*

[![CI](https://github.com/skriptedd/easyTTY/actions/workflows/ci.yml/badge.svg)](https://github.com/skriptedd/easyTTY/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/skriptedd/easyTTY)](https://github.com/skriptedd/easyTTY/releases)
[![Lizenz: MIT](https://img.shields.io/badge/Lizenz-MIT-blue.svg)](LICENSE)

Ein moderner Ersatz für PuTTY unter Windows: SSH, serielle Konsole, SFTP und RDP in
einem Programm, gemacht für Netzwerker und Admins.

*English: easyTTY is a modern PuTTY replacement for Windows with SSH, serial console,
SFTP and RDP in one app. The user interface and the documentation are in German.*

> **Mit KI entwickelt.** Dieses Projekt entsteht mit Hilfe von KI, siehe
> [Einsatz von KI](#einsatz-von-ki).

## Auf einen Blick

| | |
|---|---|
| **Verbinden** | SSH, serielle Konsole, Konsolenserver, RDP als Tab, Quick Connect |
| **Ordnen** | Sessions in Ordnern, Farben, Notizen, Suche, „Zuletzt verwendet“ |
| **Arbeiten** | Tabs, Fenster-Layouts als Blöcke in der Tableiste, sicheres Einfügen ganzer Configs |
| **Dateien** | SFTP-Browser, Firmware-Server für Switche (HTTP, TFTP, SCP/SFTP) |
| **Überblick** | Server-Status, Session-Logs mit Zeitstempel |
| **Automatisieren** | Aufgaben mit Zeitplan, KI-Zugriff per MCP mit Freigabe |
| **Mitnehmen** | Import aus PuTTY und SecureCRT, Export ohne Zugangsdaten, portable Version |

## Installation

Du brauchst Windows 10 oder 11 (64 Bit). WebView2 ist dort normalerweise schon dabei.

1. Unter [Releases](https://github.com/skriptedd/easyTTY/releases) die neueste Version laden.
2. `easyTTY-<version>.exe` starten. Eine Installation ist nicht nötig.
3. Windows warnt beim ersten Start eventuell, weil die Exe nicht signiert ist:
   „Weitere Informationen“ → „Trotzdem ausführen“.

Für Rechner ohne Adminrechte gibt es `easyTTY-<version>-portable.zip`: entpacken,
`easyTTY.exe` starten, alle Daten bleiben im Ordner.

Die ausführliche Anleitung mit Prüfsummen, Fehlerbehebung, Bauen aus dem Quellcode
und Deinstallation steht in **[docs/INSTALL.md](docs/INSTALL.md)**.

## Von PuTTY umsteigen

1. Einstellungen → „PuTTY importieren“. Sessions kommen aus der Registry, Farben
   werden nach Namensanfang vergeben.
2. Passwörter speichert easyTTY auf Wunsch verschlüsselt, Hostkeys aus
   `~\.ssh\known_hosts` werden mitgelesen.
3. Vertraute Bedienung bleibt: Markieren kopiert, Rechtsklick fügt ein.

| Du kennst aus PuTTY | In easyTTY |
|---|---|
| Gespeicherte Sessions | Ordnerbaum links, Import per Klick |
| Pageant | Windows-OpenSSH-Agent oder Schlüsseldatei (OpenSSH-Format, `.ppk` zuerst umwandeln) |
| PSCP, PSFTP | SFTP-Browser im Tab |
| Change Settings | Rechtsklick auf die Session → Bearbeiten |
| Serial | Protokoll „Serial“, erkennt Konsolenkabel |
| Special Command → Break | Tab-Rechtsklick → „Break senden“ |
| Session-Log | Automatisch mit Zeitstempel pro Zeile |

Aus SecureCRT kommst du genauso, siehe Einstellungen → „Aus anderen Programmen übernehmen“.

## Funktionen

**Verbinden**

- **SSH**: Passwort, Keyboard-Interactive, Schlüsseldatei mit Passphrase, Windows-OpenSSH-Agent, Standard-Keys (`~/.ssh/id_*`).
- **Sprung-Host und Port-Weiterleitungen**: eine gespeicherte SSH-Session als Sprung-Host (wie `ssh -J`, bis zu drei hintereinander) und lokale Weiterleitungen auf `127.0.0.1` (wie `ssh -L`).
- **Hostkeys**: Trust-on-first-use mit Fingerprint-Dialog, deutliche Warnung bei geändertem Schlüssel mit Anzeige des alten Schlüssels.
- **Serial**: COM-Port, Baud, Datenbits, Parität, Stoppbits, Break. Erkennt typische Konsolenkabel und sendet nach dem Öffnen auf Wunsch ein Enter.
- **Konsolenserver und alte Geräte** (Opengear, Avocent, Lantronix): Port im Benutzernamen, zum Beispiel `admin:6`. Alte Verfahren wie CBC und SHA-1 kommen nur zum Zug, wenn der Server nichts Neueres kann.
- **RDP**: läuft als Tab in easyTTY mit dem Remotedesktop-Steuerelement von Windows. Auflösung folgt dem Tab, Zwischenablage für die aktive Session (pro Session abschaltbar), auf Wunsch lokale Laufwerke. Rechtsklick auf die Session öffnet stattdessen mstsc.
- **Passwörter** liegen verschlüsselt per Windows DPAPI, siehe [SECURITY.md](SECURITY.md).

**Arbeiten**

- **Terminal**: xterm.js mit WebGL, Truecolor, Unicode, Suche, Zoom.
- **Tabs und Layouts**: pro Ordner ein Raster mit fester Belegung (bis 6 Fenster), eingerichtet per Drag & Drop. Jedes offene Layout ist ein Block in der Tableiste. Mehrere Layouts laufen parallel, gewechselt wird per Klick, Alt+1…9 oder Strg+Tab. Normal geöffnete Sessions bleiben eigene Tabs, hineingezogene Sessions werden ein Fenster mehr. Offene Fenster tauschen ihren Platz, wenn du sie an der Titelzeile aufeinander ziehst.
- **Sicheres Einfügen**: große Configs gehen Zeile für Zeile, die nächste erst nach dem Echo des Geräts. Mit Vorschau, Fortschritt, Abbruch und Bericht.
- **Sammeländerungen**: Rechtsklick auf einen Ordner oder „Alle Sessions“ ändert Benutzer, Passwort, Anmeldeart, Schlüssel, Keepalive oder Farbe für alle Sessions darin.
- **Namensschema**: Groß-/Kleinschreibung und Leerzeichen einheitlich halten.
- **Befehls-Schnipsel**: häufige Befehle per Klick oder Alt+J in den aktiven Tab.
- **Hervorhebungen**: IP- und MAC-Adressen, Fehler- und OK-Wörter in Farbe, eigene Regeln als Wortliste oder regulärer Ausdruck.

**Dateien und Geräte**

- **SFTP-Browser** pro Tab: Server und PC nebeneinander, Ordner, Drag & Drop aus dem Explorer, Fortschritt mit Abbruch. Downloads überschreiben nie. Bei vorhandenen Namen fragt der Upload: überschreiben, umbenennen oder überspringen. Ersetzt wird erst nach vollständiger Übertragung.
- **Firmware-Server**: gibt einen Ordner nur lesend per TFTP, HTTP und SCP/SFTP frei und zeigt fertige Befehle wie `copy tftp://…`. Auf Wunsch nimmt er Running-Configs per TFTP an.
- **Running-Config sichern**: Rechtsklick auf den Tab, auch über eine reine Konsole. Landet im wählbaren Config-Ordner.
- **Server-Status**: Last, RAM, Platten und Prozesse für Linux und Cisco IOS.
- **Session-Logs**: Klartext mit Zeitstempel pro Zeile unter `Dokumente\easyTTY\Logs`.

**Automatisieren und teilen**

- **Aufgaben**: Befehlslisten für mehrere Geräte, von Hand oder geplant. easyTTY wartet auf den Prompt, blättert durch `--More--` und erkennt `% Invalid input`. Geplante Aufgaben laufen nur, solange easyTTY läuft. Verpasste Einmal-Termine werden als „verpasst“ vermerkt, nicht nachgeholt.
- **KI-Zugriff per MCP** (aus, bis du ihn einschaltest): Claude und andere MCP-Clients können Sessions sehen und Befehle ausführen, nur von diesem PC und nur mit Token. Alles, was an ein Gerät geht, musst du in easyTTY freigeben.
- **Teilen**: Sessions, Ordner und Layouts als `.easytty.json`, ohne Benutzer, Passwörter und Pfade.

## Tastenkürzel

Alle Kürzel zeigt **Alt+K**. Die Alt-Kürzel lassen sich in den Einstellungen abschalten, dann geht Alt ans Terminal oder an den Remote-PC.

| Kürzel | Aktion |
|---|---|
| Alt+Enter, Alt+T | Quick Connect (`user@host:port`) |
| Alt+W | Tab oder Fenster schließen |
| Alt+F | Fenster im Layout groß, wieder Raster |
| Alt+Pfeile | Nachbarfenster im Layout, sonst voriger/nächster Tab |
| Alt+1 … 9 | Eintrag 1 bis 9 der Tableiste |
| Strg+Tab, Strg+Shift+Tab | Nächster, voriger Eintrag der Tableiste (die einzige Strg-Taste, Alt+Tab gehört Windows) |
| Alt+S | Seitenleiste ein/aus |
| Alt+E | In den Session-Baum und zurück |
| Alt+B | SFTP öffnen, hineinspringen, schließen |
| Alt+0, Esc | Zurück ins Terminal |
| Alt+I | Server-Status |
| Alt+U | Firmware-Server |
| Alt+A | Aufgaben |
| Alt+G | Im Terminal suchen |
| Alt+C, Alt+V | Kopieren, einfügen (auch Rechtsklick). Strg+C und Strg+V gehen wie in PuTTY ans Gerät |
| Alt+J | Befehls-Schnipsel |
| Alt+D | Tab duplizieren |
| Alt+L | Log der Sitzung |
| Alt + / −, Alt+=, Strg+Mausrad | Schriftgröße größer, kleiner, normal |
| Baum: Enter, F2, Entf | Verbinden, bearbeiten, löschen |
| Baum: Strg+Klick, Shift+Klick, Leertaste | Mehrere markieren |
| Baum: Shift+Enter | Session ins offene Layout, ohne offenes Layout neben den aktiven Tab |
| Baum: Strg+Enter | Als neuen Block starten, markierte Sessions zusammen |
| SFTP: Enter, Backspace, F2, Entf, F5, Strg+A | Öffnen, hoch, umbenennen, löschen, aktualisieren, alles markieren |

In RDP-Tabs bleiben Alt+W, E, F, S, K, T, die Ziffern und die Pfeile bei easyTTY, alle anderen Tasten gehen an den Remote-PC.

## Dokumentation

| Dokument | Inhalt |
|---|---|
| [docs/INSTALL.md](docs/INSTALL.md) | Installation, Update, Fehlerbehebung, Bauen aus dem Quellcode |
| [docs/API.md](docs/API.md) | Alle Schnittstellen: Backend, Ereignisse, MCP, Netzwerk, Dateien |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Aufbau, Services und ihre Grenzen |
| [SECURITY.md](SECURITY.md) | Sicherheitsmodell, Schwachstellen melden |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Mitmachen, Tests, Regeln |
| [CHANGELOG.md](CHANGELOG.md) | Änderungen |
| [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) | Verwendete Bibliotheken und Lizenzen |

## Einsatz von KI

easyTTY wird mit KI entwickelt. Große Teile von Code, Tests und Dokumentation sind
mit [Claude](https://claude.com/claude-code) von Anthropic entstanden, unter Anleitung
und Prüfung des Maintainers. Auch die Sicherheits- und Code-Reviews liefen mit
KI-Unterstützung. Commits, an denen die KI beteiligt war, tragen eine
`Co-Authored-By`-Zeile.

Tests und CI prüfen jede Änderung, trotzdem kann KI-erzeugter Code Fehler enthalten.
Nutze easyTTY auf produktiven Geräten mit der gleichen Vorsicht wie jedes andere
Werkzeug, und melde Fehler gern als Issue.

Davon zu trennen ist der optionale **KI-Zugriff** im Programm: Er ist standardmäßig aus
und sendet von sich aus nichts an einen KI-Dienst.

## Lizenz

[MIT](LICENSE) © 2026 skriptedd. Maintainer: skriptedd.
