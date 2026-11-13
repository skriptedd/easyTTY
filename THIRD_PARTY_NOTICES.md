# Verwendete Software

easyTTY steht unter der [MIT-Lizenz](LICENSE) und nutzt die folgenden Projekte. Die
Lizenztexte liegen den jeweiligen Projekten bei.

## Eingebunden in die Exe

| Projekt | Zweck | Lizenz |
|---|---|---|
| [Wails](https://github.com/wailsapp/wails) | Desktop-Hülle | MIT |
| [xterm.js](https://github.com/xtermjs/xterm.js) 6.0.0 mit Addons | Terminal, siehe `frontend/dist/vendor/VERSIONS.md` | MIT |
| [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) | SSH | BSD-3-Clause |
| [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) | Windows-Aufrufe | BSD-3-Clause |
| [pkg/sftp](https://github.com/pkg/sftp) | SFTP | BSD-2-Clause |
| [go.bug.st/serial](https://github.com/bugst/go-serial) | Serielle Ports | BSD-3-Clause |
| [go-ole](https://github.com/go-ole/go-ole) | COM für das RDP-Steuerelement | MIT |
| [go-winio](https://github.com/microsoft/go-winio) | Named Pipes für den SSH-Agent | MIT |
| [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | KI-Schnittstelle | Apache-2.0 und MIT |

Die vollständige Liste samt indirekter Abhängigkeiten steht in `go.mod`.

## Nur beim Bauen

| Projekt | Zweck | Lizenz |
|---|---|---|
| [go-winres](https://github.com/tc-hib/go-winres) | Icon, Manifest, Versionsangaben | 0BSD |

## Windows-Komponenten

Das Remotedesktop-Steuerelement (`mstscax.dll`) und WebView2 gehören zu Windows und
werden nicht mitgeliefert.
