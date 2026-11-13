# Mitmachen

Fehlerberichte und Ideen sind willkommen, am besten als Issue.
Bei Sicherheitslücken bitte [SECURITY.md](SECURITY.md) beachten.

## Einrichten

Du brauchst Windows, Go 1.27.4 oder neuer und Git. Node ist nicht nötig. Details stehen
in [docs/INSTALL.md](docs/INSTALL.md).

```powershell
go test ./...
go run ./tools/devsshd
go build -tags dev -o build\bin\easyTTY-dev.exe .
.\build\bin\easyTTY-dev.exe -devserver localhost:34115 -assetdir frontend\dist
```

`devsshd` ist ein SSH-Testserver auf `127.0.0.1:2222` mit Benutzer und Passwort `test`.

## Regeln für Änderungen

- Vor dem Commit müssen laufen: `gofmt -l .`, `go vet ./...`, `go test ./...`.
- Neue Funktionen und Fehlerbehebungen bekommen einen Test.
- Der Aufbau folgt [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): Fachlogik gehört in
  einen Service unter `internal/`, nicht in `App`.
- Neue Backend-Methoden, Ereignisse und MCP-Werkzeuge gehören in
  [docs/API.md](docs/API.md), ein Test prüft das.
- Oberflächentexte sind deutsch, Code und Kommentare englisch.
- Icons über `icon()` aus `ui.js`, nie als Textzeichen. Weniger ist mehr.
- Texte nur per `textContent` ins DOM, nie als HTML.
- Commit-Nachrichten: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`.

KI-Werkzeuge sind erlaubt und erwünscht. Du bleibst für den Beitrag verantwortlich:
Verstehe und prüfe, was du einreichst, und nenne die KI im Commit
(`Co-Authored-By`).

## Tests

| Bereich | Wie getestet |
|---|---|
| Services unter `internal/` | Unit- und Integrationstests, SSH gegen `internal/sshtest` |
| Schnittstellen | `internal/apicontract` gleicht Oberfläche, Backend und Doku ab |
| Wurzelpaket | Nur unter Windows |
| RDP | Unit-Tests, echtes Steuerelement mit `EASYTTY_RDP_SMOKE=1` |
| Oberfläche | Noch ohne automatische Tests, von Hand mit `devsshd` prüfen |

## Release

Tag pushen, CI baut Exe und Portable-Zip und legt das Release an:

```bash
git tag v1.0.0
```

```bash
git push origin v1.0.0
```
