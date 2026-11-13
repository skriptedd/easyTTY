// All keyboard shortcuts in one place: shown in the help dialog (Alt+K).
// Every app shortcut is Alt+key (Super belongs to Windows, so Alt it is;
// itself); Strg combos belong to the terminal.
import { h, modal } from "./ui.js";

export const SHORTCUTS = [
  ["Alles mit Alt", [
    [["Alt", "Enter"], "neue Verbindung (Quick Connect, auch Alt+T)"],
    [["Alt", "W"], "Tab / Fenster schließen"],
    [["Alt", "F"], "Fenster im Layout groß / wieder Raster"],
    [["Alt", "← → ↑ ↓"], "Nachbarfenster im Layout · sonst Tab links/rechts"],
    [["Alt", "1 … 9"], "Tab oder Layout 1 bis 9"],
    [["Alt", "S"], "Seitenleiste ein/aus"],
    [["Alt", "U"], "Firmware-Server (Images per TFTP/HTTP/SCP an Switche)"],
    [["Alt", "A"], "Aufgaben & Zeitpläne (Befehle auf Geräten ausführen)"],
    [["Alt", "K"], "diese Übersicht"],
  ]],
  ["Springen", [
    [["Alt", "E"], "in den Session-Baum (nochmal: zurück ins Terminal)"],
    [["Alt", "B"], "SFTP öffnen → hineinspringen → schließen"],
    [["Tab"], "im SFTP: Server ↔ Dieser PC"],
    [["Esc"], "aus Baum oder SFTP zurück ins Terminal"],
    [["Alt", "0"], "von überall ins Terminal"],
  ]],
  ["Terminal", [
    [["Alt", "I"], "Server-Status (Last, RAM, Prozesse) ein/aus"],
    [["Alt", "G"], "im Terminal suchen"],
    [["Alt", "J"], "Befehls-Schnipsel (dann 1 … 9 oder Pfeile und Enter)"],
    [["Alt", "C"], "kopieren (Markieren kopiert auch automatisch)"],
    [["Alt", "V"], "einfügen (auch Rechtsklick)"],
    [["Alt", "D"], "Tab duplizieren"],
    [["Alt", "L"], "Log dieser Sitzung zeigen"],
    [["Esc"], "laufendes Einfügen abbrechen"],
    [["Alt", "+ / −"], "Schrift größer / kleiner, Alt+= normal (auch Strg+Mausrad)"],
  ]],
  ["Baum & Fenster-Layout", [
    [["▶ ▦"], "am Ordner: alle Sessions in ihren Fenstern öffnen"],
    [["Ziehen"], "Session oder Tab in die Ansicht ziehen: ein Fenster mehr (1 → 2 → 3 → 4 → 6)"],
    [["Enter"], "Session verbinden · im getrennten Tab: neu verbinden"],
    [["Shift", "Enter"], "im Baum: Session ins offene Layout, der Fokus bleibt im Baum"],
    [["Strg", "Enter"], "im Baum: als neuen Block starten, markierte zusammen"],
    [["Leertaste"], "im Baum: Session markieren"],
    [["F2"], "Session bearbeiten"],
    [["Entf"], "Session/Ordner löschen (Ordner: fragt, ob die Sessions mit gehen)"],
    [["Strg", "Klick"], "mehrere Sessions markieren (Shift+Klick: Bereich, Strg+A: alle)"],
    [["Strg", "Enter"], "im Quick Connect: als Session speichern"],
  ]],
  ["SFTP-Browser", [
    [["Enter"], "Ordner öffnen / Datei auf die andere Seite übertragen (auch Doppelklick)"],
    [["Backspace"], "eine Ebene hoch"],
    [["F2"], "umbenennen"],
    [["Entf"], "löschen"],
    [["F5"], "aktualisieren"],
    [["Strg", "A"], "alles markieren"],
  ]],
  ["Ausnahme", [
    [["Strg", "Tab"], "nächster / mit Shift vorheriger Tab (Alt+Tab gehört Windows)"],
    [["Strg", "Shift", "Buchstabe"], "dieselben Kürzel, wenn die Alt-Kürzel in den Einstellungen aus sind"],
  ]],
];

const keyCombo = (keys) => keys.flatMap((k, i) => [i ? "+" : null, h("kbd", {}, k)]);

export function showShortcuts() {
  return modal((close) => ({
    head: "Tastenkürzel",
    wide: true,
    body: h("div", { class: "shortcut-groups" },
      SHORTCUTS.map(([group, rows]) => h("div", { class: "shortcut-group" },
        h("div", { class: "shortcut-title" }, group),
        rows.map(([keys, label]) => h("div", { class: "shortcut-row" },
          h("span", { class: "shortcut-keys" }, keyCombo(keys)),
          h("span", {}, label)))))),
    foot: [h("button", { class: "btn primary", onclick: () => close() }, "Schließen")],
  }));
}
