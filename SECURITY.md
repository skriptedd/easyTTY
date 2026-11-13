# Sicherheit

## Schwachstellen melden

Bitte keine öffentlichen Issues für Sicherheitslücken. Nutze auf GitHub
**Security → Report a vulnerability**. Beschreibe, wie sich das Problem nachstellen
lässt und welche Version betroffen ist. Gepflegt wird nur die neueste Version.

## Sicherheitsmodell

easyTTY läuft als normales Programm unter deinem Windows-Konto und braucht keine
Adminrechte. Wer bereits Code unter deinem Konto ausführen kann, ist kein Gegner,
vor dem easyTTY schützen kann.

### Passwörter

- Liegen pro Session in `sessions.json`, verschlüsselt mit Windows DPAPI.
- Der Schlüssel hängt an deinem Windows-Konto. Es gibt kein Master-Passwort.
- Auf einem anderen PC oder Konto lassen sie sich nicht entschlüsseln, easyTTY fragt
  sie dann neu ab.
- Die Oberfläche bekommt sie nie, nur die Info „Passwort vorhanden“.
- Exporte enthalten weder Passwörter noch Benutzernamen, Schlüssel- oder Log-Pfade.
- Einmal-Codes (OTP) werden nicht gespeichert.

Schlüssel-Passphrasen speichert easyTTY nicht.

### SSH

- Es gibt keinen Weg, die Hostkey-Prüfung abzuschalten.
- Unbekannte Schlüssel müssen bestätigt werden, geänderte lösen eine deutliche Warnung
  aus. Aufgaben ohne Benutzer brechen in beiden Fällen ab.
- Ein bekannter Host wird auf seine Schlüsseltypen festgelegt.
- Alte Verfahren (CBC, 3DES, SHA-1, DSA) stehen für alte Geräte bereit, aber immer
  hinter den modernen. Der ausgehandelte Hash ist vom Hostkey signiert, ein Angreifer
  kann also bei bekannten Hosts nicht auf alte Verfahren herunterzwingen. Ein Gerät,
  das nur Altes kann, wird trotzdem verbunden: Bei solchen Geräten gilt die Verbindung
  als schwach geschützt.
- Agent-Forwarding und X11 werden nicht genutzt.

### KI-Zugriff (MCP)

Standardmäßig aus. Eingeschaltet gilt:

- Lauscht nur auf `127.0.0.1` und verlangt ein Token. Das Token liegt im Klartext in
  `settings.json`. Prozesse unter deinem Konto können es lesen, sie müssen aber
  trotzdem jede Änderung von dir freigeben lassen.
- Alles, was an ein Gerät geht oder Aufgaben ändert, wartet auf deine Freigabe.
  Eine 15-Minuten-Freigabe gilt nur für diesen Client und genau diesen Inhalt.
- Terminal-Inhalte und gespeicherte Befehlsausgaben sieht die KI nur mit „Tabs lesen“.
- Die Option „show-Befehle ohne Rückfrage“ ist aus. Beachte: `show running-config`
  kann Geheimnisse ausgeben.
- Befehle mit Steuer- oder unsichtbaren Zeichen werden abgelehnt.

### Firmware-Server

Läuft nur, wenn du ihn startest. Geräte können Dateien nur lesen, außer du schaltest
„Configs annehmen“ ein. Dann darf jeder im Netz per TFTP Dateien bis 64 MB in den
Config-Ordner schreiben (nie überschreibend, nur dorthin). Der Config-Ordner darf nicht
im Firmware-Ordner liegen, damit niemand die Configs lesen kann. Schalte das nur für die
Sicherung ein. **HTTP und TFTP haben keine
Anmeldung.** Solange er läuft, kann jeder im Netz die Dateien im gewählten Ordner
lesen. Wähle daher eine Netzwerkkarte statt „alle“ und einen Ordner nur mit Images.

### Sprung-Host und Port-Weiterleitungen

- Jeder Sprung-Host prüft seinen Hostkey wie eine normale Verbindung und meldet sich
  mit seiner eigenen gespeicherten Anmeldung an. Ein dort eingetipptes Passwort wird
  nie für das Ziel gespeichert.
- Weiterleitungen lauschen nur auf `127.0.0.1`, also nicht für andere im Netz.
  Programme unter jedem Konto dieses PCs erreichen sie aber, solange die Session
  verbunden ist.
- easyTTY leitet keinen SSH-Agent weiter.

### Import fremder Dateien

Importierte Sessions bringen weder Benutzer noch Pfade mit, lokale Laufwerke sind bei
RDP aus. Importierte Einstellungen ändern keine KI-, Log- und Sicherheitsschalter.
Prüfe trotzdem, wohin eine fremde Session verbindet, bevor du dich anmeldest.

### Session-Logs

Sind standardmäßig an und enthalten alles, was das Gerät ausgibt, also auch Configs
mit Hashes oder SNMP-Strings. Eingaben ohne Echo wie Passwörter landen nicht im Log.
Ist dein Dokumente-Ordner mit OneDrive verbunden, wandern die Logs in die Cloud.
Der Ordner lässt sich in den Einstellungen ändern.

### Release

- Die Exe hat keine Windows-Signatur (Code-Signing), darum warnt SmartScreen. Prüfe
  sie über `SHA256SUMS.txt`.
- Beim Start fragt easyTTY die Release-Seite nach einer neuen Version (abschaltbar
  in den Einstellungen). Dabei wird nichts gesendet außer der Versionsnummer im
  User-Agent. Installiert wird erst nach deiner Bestätigung.
- Jedes Release signiert `SHA256SUMS.txt` mit einem Ed25519-Schlüssel
  (`SHA256SUMS.txt.sig`). Der geheime Teil liegt nur in den Secrets des
  Release-Laufs, der öffentliche ist in easyTTY eingebaut. Ein Update kommt nur per
  HTTPS von der Release-Seite, die Signatur muss stimmen und die Exe zur Summe passen.
  Wer nur Dateien ins Release laden kann, kann so kein Update unterschieben.
- Vor jedem Update sichert easyTTY Sessions, Einstellungen, Hostkeys und Aufgaben in
  den Datenordner (`backups`), die vorige Exe bleibt als `easyTTY.old.exe`.
- Es gibt keine Telemetrie.
- easyTTY lädt DLLs nur aus System32.
- Abhängigkeiten sind per `go.sum` festgelegt, CI prüft mit `govulncheck`.
