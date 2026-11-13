# Änderungen

## 0.3.1 (2026-11-13)

### Behoben

- Nach einem Update war das an die Taskleiste angeheftete Symbol weg. Das Update
  benennt `easyTTY.exe` nicht mehr um, sondern schreibt die neue Version in dieselbe
  Datei, sobald die alte beendet ist. Wirkt ab dem Update, das von dieser Version aus
  startet.

## 0.3.0 (2026-11-13)

Erste öffentliche Version auf GitHub.

### Neu

- Updates sind signiert: Die Update-Funktion nimmt nur Releases an, deren Prüfsummen
  mit dem Schlüssel des Projekts signiert sind.
- Vor jedem Update sichert easyTTY Sessions, Einstellungen, Hostkeys und Aufgaben nach
  `backups` im Datenordner (die letzten fünf). Die vorige Exe bleibt als
  `easyTTY.old.exe` für den Weg zurück.

### Geändert

- Oberfläche ohne Vergleiche mit anderen Programmen und ohne Herstellernamen. Nur die
  Import-Knöpfe nennen ihr Quellformat.

## 0.2.19 (2026-11-12)

### Neu

- Hervorhebungen im Terminal: IP-Adressen, MAC-Adressen, Fehler- und OK-Wörter in Farbe.
  Eigene Regeln (Wortliste oder regulärer Ausdruck, sechs Farben) in den Einstellungen,
  mit Vorschau. Färbt nur die Anzeige, Logs bleiben unverändert.

### Behoben

- Portable in OneDrive: Der Zwischenspeicher der Oberfläche liegt jetzt außerhalb von
  OneDrive, die Meldung „Elementname oder -typ nicht zulässig“ für `cache.db` entfällt.

## 0.2.18 (2026-11-12)

### Neu

- Configs vergleichen: zwei gesicherte Running-Configs Zeile für Zeile, Entferntes rot,
  Neues grün. Im Tab-Menü und direkt nach dem Sichern („Vergleichen“ nimmt die vorige
  Sicherung desselben Geräts). Zeitstempel-Zeilen bleiben auf Wunsch außen vor.
- Einstellungen mit Sprungleiste zu den Abschnitten.
- Im Baum zeigt ↪ Sessions mit Sprung-Host oder Port-Weiterleitungen, Details im Tooltip.
- Der Hostkey-Dialog nennt einen Sprung-Host als solchen.
- „Zuletzt verwendet“ im Baum lässt sich einklappen (Klick auf die Überschrift), der Zustand
  bleibt gespeichert.
- Sind die Alt-Kürzel abgeschaltet, liegen dieselben Kürzel auf Strg+Shift+Buchstabe.

### Behoben

- Hinter einem Sprung-Host hing die Verbindung endlos, wenn das Ziel nicht antwortete.
- Einfügen: Ein Prompt wie `sw-more#` wurde für „--More--“ gehalten und mit Leerzeichen
  beantwortet.
- TFTP-Uploads behalten keine ausführbaren Endungen (`.bat`, `.lnk`, `.exe` bekommen `.txt`).
- Import: Sprung-Hosts im Kreis oder über Nicht-SSH-Sessions werden entfernt.
- Einstellungen speichern überschrieb eine eigene Schriftliste mit Ersatzschriften.
- Schnipsel starten nicht mehr mitten in ein laufendes Einfügen. Esc im Schnipsel-Menü gibt
  die Tastatur ans Terminal zurück.

## 0.2.17 (2026-11-11)

### Neu

- Nach „Running-Config sichern“ öffnet die Meldung die Datei oder zeigt sie im Ordner.
- Der Config-Ordner lässt sich in den Einstellungen wählen (Abschnitt „Running-Configs“).
- Einfügen mit „Ausgabe in Datei“: Die Zeilen laufen als Befehle, die Ausgabe landet als
  Textdatei im Config-Ordner.
- Schnipsel mit „Ausgabe in Datei“: Die Befehle laufen nacheinander, easyTTY blättert selbst
  um und speichert Befehle und Ausgaben als Textdatei im Config-Ordner.

### Behoben

- Alt allein zeigt im Terminal keinen Kreuz-Mauszeiger mehr. Rechteckig markieren geht
  weiter mit Alt+Ziehen.

## 0.2.16 (2026-11-11)

### Neu

- Versionsanzeige unten rechts in der Statusleiste (Klick sucht nach Updates) und auf
  dem Startbildschirm.
- Jeder Dialog hat oben rechts ein ✕ zum Schließen (wie Esc oder Abbrechen).
- Einfüge-Dialog aufgeräumt: Weglassen-Optionen als Schalter in einer Zeile, Sendeart als
  zwei Knöpfe, die Pause erscheint nur beim zeilenweisen Senden.
- Helles Design weniger grell: Farben von Catppuccin Latte (graublau statt Reinweiß),
  auch im Terminal.

### Behoben

- Fenster und Taskleiste zeigen das easyTTY-Logo statt eines leeren Symbols. Das Logo
  lag im Programm unter einem Namen, das Fenster sucht es unter der Nummer 3.

## 0.2.15 (2026-11-11)

### Behoben

- Gebaut mit Go 1.27.4: schließt GO-2026-6859, einen Absturz des HTTP-Servers (betrifft
  den KI-Zugang und den Firmware-HTTP-Server). Sonst wie 0.2.14.

## 0.2.14 (2026-11-11)

### Neu

- Updates: easyTTY sucht beim Start nach einer neuen Version, lädt sie auf Wunsch,
  prüft die SHA-256-Summe, ersetzt die Exe und startet neu. Abschaltbar in den
  Einstellungen, dort auch „Jetzt nach Updates suchen“.
- Befehls-Schnipsel: häufige Befehle per Klick oder Alt+J in den aktiven Tab,
  verwalten im selben Menü.
- Schriftart als Auswahlliste der installierten Monospace-Schriften, mit Vorschau.
- SFTP-Upload fragt bei vorhandenen Namen: Überschreiben, Umbenennen oder Überspringen.
- RDP: Zwischenablage pro Session abschaltbar.
- SSH: Sprung-Host (wie `ssh -J`): eine gespeicherte SSH-Session, über die die Verbindung
  läuft, bis zu drei hintereinander. Gilt auch für Aufgaben.
- SSH: lokale Port-Weiterleitungen (wie `ssh -L`) auf `127.0.0.1`, solange die Session
  verbunden ist.
- Running-Config sichern: Rechtsklick auf den Tab liest sie über die offene Verbindung,
  auch über eine reine Konsole. Oder der Firmware-Server nimmt sie per TFTP an
  (`copy running-config tftp:`). Beides landet im wählbaren Config-Ordner, nichts wird
  überschrieben.
- Einfügen: Zeilen mit `!` und mit `#` lassen sich getrennt weglassen.
- Kleine Leisten und Meldungen schließen mit ✕ oben rechts. Beim Einfügen bringt
  „⤓ Nach unten“ zur aktuellen Zeile.

### Geändert

- Alle App-Kürzel sind Alt+Taste: Kopieren Alt+C, Einfügen Alt+V, Schrift Alt+Plus/Minus.
  Strg+V geht wie in PuTTY ans Gerät, die alten Strg+Shift-Kürzel sind weg. Nur Strg+Tab
  bleibt, weil Alt+Tab zu Windows gehört.

### Behoben

- Einfügen mehrerer Zeilen: Nach einem `show`-Befehl mit „--More--“ verschluckte der
  Switch den ersten Buchstaben der nächsten Zeile („how version“). easyTTY wartet jetzt
  auf den Prompt und blättert selbst weiter.
- TFTP: Blöcke höchstens 8192 Byte und ein Limit pro Gerät, damit der Server im LAN
  nicht als Verstärker taugt und das Protokoll nicht überläuft.
- `settings.json` wird nur noch von einer Stelle gleichzeitig geändert.
- RDP: mögliche Hänger beim Schließen des Fensters und während eines
  Anmeldedialogs behoben, OLE wird für Zwischenablage und Laufwerke richtig gestartet.
- RDP: Ein Tab, der während der Anmeldung geschlossen wurde, ließ eine unsichtbare
  Verbindung offen.
- Fenster-Layout: Der Wechsel auf ein kleineres Raster und zurück verliert keine
  Sessions mehr.
- Die Sicherungskopie (`.bak`) von Sessions und Einstellungen wird in einem Schritt
  ersetzt und nie mit einer leeren Datei überschrieben.
- SFTP-Upload auf Server ohne `posix-rename`: Scheitert das Ersetzen, bleibt die
  bisherige Datei erhalten statt gelöscht zu werden.
- Einfügen entfernt Steuerzeichen aus der Zwischenablage (Esc, Strg+C, Strg+D).
- SecureCRT-Import übernimmt keine Schlüsseldateien von Netzwerkpfaden (`\\server\…`).
- KI-Freigabedialog und Tab-Liste zeigen Tab-Titel ohne versteckte Zeichen.
- Der SFTP-Zugang des Firmware-Servers listet keine versteckten Ordner mehr auf.

## 0.2.13 (2026-11-06)

### Neu

- Fenster-Layouts per Drag & Drop einrichten, offene Fenster an der Titelzeile tauschen.
- Das App-Logo steht in der Kopfzeile und auf dem Startbildschirm.
- Im Baum per Tastatur: Shift+Enter bringt Sessions ins offene Layout, Strg+Enter startet einen neuen Block, die Leertaste markiert.

### Behoben

- „Für alle Sessions ändern“ überschreibt die Konsolenserver-Line nicht mehr: aus `ops:6` wird mit neuem Benutzer `admin:6`.

## 0.2.12 (2026-11-05)

Vorbereitung auf die Veröffentlichung.

### Neu

- Tableiste mit Layout-Blöcken: mehrere Layouts parallel, wechseln per Klick, Alt+1…9
  oder Strg+Tab. Normal geöffnete Sessions gehen nicht mehr ins Layout.
- „Break senden“ im Tab-Menü von Serial-Sessions.
- Sessions lassen sich auch auf RDP-Tabs ziehen und landen so im selben Layout.
- Abstürze landen in `crash.log` im Datenordner.
- Alt+F und Alt+Pfeile in RDP-Tabs.
- Dokumentation: Installation, Schnittstellen, Architektur, Sicherheit.

### Geändert

- Tooltip im Baum zeigt den Session-Namen.
- RDP-Zwischenablage geht nur noch an die aktive Session.
- Das Fenster fragt beim Beenden auch bei offenen RDP-Tabs nach.
- Importierte Einstellungen ändern keine Sicherheits-, Log- und KI-Schalter mehr.
- RDP-Felder für Bildschirmgröße und Monitore entfernt, RDP folgt dem Tab.

### Behoben

- Einmal-Codes konnten als Passwort gespeichert werden.
- `known_hosts` konnte bei einem Lesefehler geleert werden.
- Abgebrochene SFTP-Uploads beschädigten die Zieldatei.
- Downloads: Namen, die sich nur in Groß-/Kleinschreibung unterscheiden, überschrieben
  sich, eine vorhandene `.part`-Datei ging verloren, ein unlesbarer Ordner brach alles ab.
- Eine importierte RDP-Session konnte lokale Laufwerke freigeben.
- KI-Clients konnten gespeicherte Ausgaben ohne „Tabs lesen“ abrufen.
- Keepalive erkannte hängende Verbindungen nicht.
- Fehler eines gewählten SSH-Schlüssels wurden verschluckt.
- Aufgaben: fehlerhaftes Speichern, verpasste Einmal-Termine, abgeschnittene Ausgaben.
- Viele kleinere Fehler in Oberfläche und Fokus.

### Sicherheit

- DLLs werden nur noch aus System32 geladen.
- Content-Security-Policy für die Oberfläche.
- Systemprogramme werden mit vollem Pfad gestartet.

## 0.2.x

Frühere Versionen wurden nicht öffentlich veröffentlicht.
