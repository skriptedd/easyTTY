# Änderungen

## 0.3.10 (2026-11-24)

### Neu

- Lokale Shell: PowerShell, PowerShell 7 oder cmd auf diesem PC als Tab, über Windows'
  Pseudo-Konsole (Farben, PSReadLine, Größenänderung). Alt+P oder „›_ PowerShell“ oben öffnet
  sofort eine PowerShell (Rechtsklick: andere Shell). Als Session vom Typ „Lokal“ gespeichert
  auch mit eigenem Startordner, mit Session-Log wie bei SSH.
- Lokale Tabs und Sessions sind für KI-Zugriff (MCP) und Aufgaben gesperrt: nicht gelistet,
  nicht lesbar, nichts hineintippbar.

## 0.3.9 (2026-11-24)

### Neu

- Neue Session per Tastatur: Alt+H öffnet „Neue Session“ im markierten Ordner (ohne
  Alt-Kürzel: Strg+Shift+H).

## 0.3.8 (2026-11-24)

### Neu

- „Logs & Dateien“ (Alt+L, 📄 in der Tab-Leiste): alle Session-Logs, gesicherten Configs und
  Schnipsel-Ausgaben in einer Liste, neueste zuerst, mit Suche und Filter. Enter öffnet die
  Datei, Shift+Enter zeigt sie im Explorer; das Log der aktuellen Sitzung ist markiert.
- Nach einem Schnipsel mit „Ausgabe in Datei“ bietet die Meldung neben „Öffnen“ und
  „Im Ordner zeigen“ auch „In der Liste“ an. Die Ausgaben liegen jetzt im Unterordner
  `Ausgaben` des Config-Ordners und lassen sich weiterhin vergleichen.
- Tastenkürzel (Alt+K) lassen sich durchsuchen, nach Beschreibung („log“) oder nach Taste
  („Alt L“, „Strg F“), und sind größer und besser lesbar.
- Neuer Ordner per Tastatur: Alt+N (im SFTP-Browser dort, sonst im Session-Baum), F7 im Baum
  und in beiden SFTP-Listen. „Dieser PC“ im SFTP-Browser kann jetzt auch Ordner anlegen.
- Tab oder Session in die Ansicht ziehen zeigt vorher, wo sie landet: an den linken, rechten,
  oberen oder unteren Rand gezogen öffnet sie daneben bzw. darüber oder darunter.
- Einstellung „Nach dem Anmelden per SSH automatisch Enter senden“ für Geräte, die ihren
  Prompt erst nach einer Taste zeigen.
- Das Fenster startet so, wie es geschlossen wurde: maximiert oder an seiner letzten Stelle.

### Behoben

- Portabel: Nach dem Verschieben oder Kopieren des Ordners legte easyTTY die Logs weiter am
  alten Ort an. Log- und Config-Ordner im Datenordner werden jetzt relativ gespeichert, alte
  Pfade auf den aktuellen Ordner umgebogen.
- Das Fenster konnte unsichtbar starten: außerhalb des Bildschirms, größer als der Bildschirm
  (Titelleiste nicht erreichbar) oder hinter anderen Fenstern. Es wird jetzt immer auf einen
  Bildschirm gelegt und nach vorn geholt.
- Speichern scheiterte noch selten mit „Zugriff verweigert“, wenn die Datei gerade gelesen wurde.

## 0.3.7 (2026-11-23)

### Behoben

- SSH: Nach jeder Neuaushandlung der Schlüssel (etwa alle 1 GB oder zeitgesteuert vom Server)
  wurde die Verbindung 20 s später getrennt und als normales „Verbindung beendet“ gemeldet.
- SSH: Eine fehlerhafte Zeile in `~/.ssh/known_hosts` ließ alle dort bekannten Hosts als
  „unbekannt“ gelten – ein geänderter Hostkey kam dann ohne Warnung als normale Rückfrage.
  Jetzt wird nur die kaputte Zeile übergangen.
- SSH: Ein Abbruch wegen fehlender Keepalive-Antwort wird als Fehler gemeldet statt als normales Ende.
- Seriell: „Neu verbinden“ zeigte auf der wieder laufenden Verbindung „Verbindung beendet“
  und die Neu-verbinden-Leiste.
- SFTP: Dateien aus dem Explorer auf einen Server-Ordner gezogen landeten bei sortierter oder
  gefilterter Liste im falschen Ordner.
- SFTP: Zwei Downloads gleichen Namens in denselben Ordner konnten sich überschreiben.
- SFTP: Eine hochgeladene Datei ersetzt keinen gleichnamigen Ordner auf dem Server mehr.
- SFTP: Server-Dateien aus einem anderen Tab (Fensterteilung) lassen sich nicht mehr auf den
  falschen Server ziehen.
- Speichern scheiterte gelegentlich, wenn gerade ein anderer Zugriff (Virenscanner, Sync-Tool,
  zweites Lesen) die Datei offen hatte. Kurz unlesbare Einstellungen fielen dabei auf die
  Standardwerte zurück (z. B. Logging wieder an).
- Nach dem Rücksprung auf `sessions.json.bak` überschrieb das nächste Speichern die gute
  Sicherung mit der beschädigten Datei.
- Sprung-Hosts: Eine Session, über die andere springen, lässt sich nicht mehr so ändern, dass
  diese nicht mehr speicherbar sind. „Schema jetzt anwenden“ bricht nicht mehr bei der ersten
  nicht speicherbaren Session ab.
- Import: Ordnernamen mit Leerzeichen am Rand wurden bei jedem erneuten Import neu angelegt.
- Einstellungen: „Speichern“ nach einem Import aus dem Einstellungsfenster machte die
  importierten Einstellungen rückgängig.
- Dialoge halten den Tastaturfokus: Tab springt nicht mehr hinaus, Tippen landet nie im
  Terminal dahinter.
- Hervorhebung: Ein Fenstertitel (OSC), der über zwei Datenpakete ging, wurde zerstückelt
  und teils ins Terminal geschrieben.
- KI-Zugriff: Ein Tabulator vor einer Option (`display<Tab>-write …`) galt als harmloser
  show-Befehl und lief ohne Freigabe.
- Firmware-Server: TFTP-Uploads sind pro Serverstart auf 500 Dateien und 1 GB begrenzt, ein
  Upload zeigt keine MD5 einer gleichnamigen Firmware-Datei mehr, versteckte Dateien stehen
  nicht mehr in der Dateiliste.
- Layout-Editor: Fenster mit gelöschten Sessions gelten als frei.

## 0.3.6 (2026-11-20)

### Behoben

- Hervorhebungen trafen bei stückweise ankommender Ausgabe (langsame Geräte, serielle Konsole)
  oft nur einen Teil: Von einer IP blieben die letzten Ziffern ungefärbt, „up“, „connected“ oder
  „notconnect“ wurden mal markiert und mal nicht. Ein angeschnittenes Wort wartet jetzt, bis es
  vollständig ist (bis 40 ms Ruhe, höchstens 250 ms); nur das Echo getippter Tasten erscheint
  sofort. Auch `10.0.0.0/24` und `Gi1/0/1` über einer Paketgrenze werden richtig erkannt.

## 0.3.5 (2026-11-20)

### Neu

- SFTP: Klick auf „Name“, „Größe“ oder „Geändert“ sortiert die Dateiliste, ein zweiter Klick dreht
  die Reihenfolge um. Ordner bleiben oben, die Sortierung wird gemerkt.
- SFTP: Filterfeld über jeder Liste (Strg+F), mehrere Wörter, `*` und `?` als Platzhalter (z. B. `*.log`).
  Esc zeigt wieder alle. Löschen und Übertragen betreffen nur, was sichtbar ist.
- SFTP: Umschalt+Enter im Filterfeld (oder der Knopf darunter) sucht im Server-Ordner und allen
  Unterordnern. Treffer stehen mit ihrem Pfad in der Liste und lassen sich herunterladen, umbenennen,
  löschen oder per Rechtsklick „Im Ordner zeigen“. Esc oder Backspace führt zurück. Höchstens
  1000 Treffer, 20 000 Ordner und 2 Minuten; Links auf Ordner werden nicht betreten.

### Behoben

- Lief easyTTY schon länger, bot es beim Update noch die Version vom Programmstart an und
  scheiterte mit „Version … wird nicht mehr angeboten“. Der Update-Dialog fragt jetzt beim
  Öffnen nach der aktuellen Version und bietet bei einem Fehlschlag gleich die neuere an.

## 0.3.4 (2026-11-18)

### Neu

- Neuer Ordner fragt gleich, welche Sessions hinein sollen, wahlweise kopiert oder verschoben.
- Rechtsklick auf einen Ordner → „Sessions hinzufügen…“. Rechtsklick auf eine oder mehrere
  markierte Sessions → „In Ordner kopieren / verschieben…“ und „Neuer Ordner mit diesen Sessions…“.
- Ziehen nimmt alle markierten Sessions mit, mit gedrückter Strg-Taste werden sie kopiert.
  Kopien behalten ihr gespeichertes Passwort.

## 0.3.3 (2026-11-18)

### Neu

- Gibt es beim Start eine neue Version, meldet easyTTY das jetzt deutlich mit „Ansehen“
  und „Später“. Bisher stand es nur klein in der Statusleiste.

## 0.3.2 (2026-11-17)

### Neu

- Rechtsklick auf einen SSH-Tab → „SSH-Verbindung“: ausgehandelte Verfahren (Schlüsseltausch,
  Verschlüsselung und MAC je Richtung, Hostkey-Verfahren), Hostkey mit Typ, Länge und
  Fingerabdruck, Anmeldeart, benutzte Schlüsseldatei und Sprung-Hosts. Veraltete
  Verfahren sind markiert.
- Ausgabe in Datei: auf Wunsch eine Datei pro Befehl und die Dateien gleich öffnen
  (Einstellungen → Configs und Ausgaben).
- Einfügen-Fenster: Enter sendet, Alt+Enter (oder Shift+Enter) macht eine neue Zeile.

### Behoben

- SFTP: Mehrere markierte Dateien lassen sich wieder zusammen ziehen. Bisher hat der
  Klick zum Ziehen die Auswahl auf eine Datei zurückgesetzt. Aus dem Explorer auf einen
  Server-Ordner gezogene Dateien landen in diesem Ordner.
- Hervorhebungen: Ein Wort wie „up“ wurde manchmal mitten in „group“ oder „update“
  gefärbt, wenn die Ausgabe genau dort in zwei Stücken ankam.

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
