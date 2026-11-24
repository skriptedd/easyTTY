// All keyboard shortcuts in one place: shown in the help dialog (Alt+K),
// with a search box. Every app shortcut is Alt+key (Super belongs to
// Windows); Strg combos belong to the terminal.
import { h, modal } from "./ui.js";

export const SHORTCUTS = [
  ["Allgemein", [
    [["Alt", "Enter"], "Neue Verbindung (Quick Connect, auch Alt+T)"],
    [["Alt", "W"], "Tab oder Fenster schließen"],
    [["Alt", "1 … 9"], "Tab oder Layout 1 bis 9"],
    [["Strg", "Tab"], "Nächster Tab, mit Shift der vorherige (Alt+Tab gehört Windows)"],
    [["Alt", "S"], "Seitenleiste ein/aus"],
    [["Alt", "L"], "Logs & Dateien: Logs, Configs, Schnipsel-Ausgaben suchen und öffnen"],
    [["Alt", "H"], "Neue Session anlegen (im markierten Ordner)"],
    [["Alt", "P"], "PowerShell auf diesem PC (Rechtsklick auf den Knopf oben: PowerShell 7, cmd)"],
    [["Alt", "N"], "Neuer Ordner (im SFTP dort, sonst im Session-Baum)"],
    [["Alt", "U"], "Firmware-Server (Images per TFTP/HTTP/SCP an Switche)"],
    [["Alt", "A"], "Aufgaben und Zeitpläne (Befehle auf Geräten ausführen)"],
    [["Alt", "K"], "Diese Übersicht"],
  ]],
  ["Springen", [
    [["Alt", "E"], "In den Session-Baum, nochmal: zurück ins Terminal"],
    [["Alt", "B"], "SFTP öffnen, hineinspringen, schließen"],
    [["Alt", "0"], "Von überall ins Terminal"],
    [["Esc"], "Aus Baum oder SFTP zurück ins Terminal"],
  ]],
  ["Terminal", [
    [["Alt", "C"], "Kopieren (Markieren kopiert auch automatisch)"],
    [["Alt", "V"], "Einfügen (auch Rechtsklick)"],
    [["Alt", "G"], "Im Terminal suchen"],
    [["Alt", "J"], "Befehls-Schnipsel, dann 1 … 9 oder Pfeile und Enter"],
    [["Alt", "D"], "Tab duplizieren"],
    [["Alt", "I"], "Server-Status (Last, RAM, Prozesse) ein/aus"],
    [["Alt", "+"], "Schrift größer (Alt+− kleiner, Alt+= normal, auch Strg+Mausrad)"],
    [["Esc"], "Laufendes Einfügen abbrechen"],
  ]],
  ["Fenster-Layout", [
    [["Alt", "F"], "Fenster im Layout groß, nochmal: wieder Raster"],
    [["Alt", "← → ↑ ↓"], "Nachbarfenster im Layout, sonst Tab links/rechts"],
    [["Ziehen"], "Tab oder Session an einen Rand der Ansicht ziehen: daneben oder darunter öffnen"],
    [["Ziehen"], "Auf ein Fenster im Layout: Plätze tauschen; auf „frei“: dort öffnen"],
    [["▶ ▦"], "Am Ordner: alle Sessions in ihren Fenstern öffnen"],
  ]],
  ["Session-Baum", [
    [["Enter"], "Session verbinden, im getrennten Tab: neu verbinden"],
    [["Shift", "Enter"], "Session ins offene Layout, der Fokus bleibt im Baum"],
    [["Strg", "Enter"], "Als neuen Block starten, markierte zusammen"],
    [["Leertaste"], "Session markieren"],
    [["Strg", "Klick"], "Mehrere markieren (Shift+Klick: Bereich, Strg+A: alle)"],
    [["F2"], "Session bearbeiten, Ordner umbenennen"],
    [["F7"], "Neuer Ordner (unter dem markierten Ordner)"],
    [["Entf"], "Session oder Ordner löschen (fragt bei Ordnern, ob die Sessions mitgehen)"],
    [["Strg", "Enter"], "Im Quick Connect: als Session speichern"],
  ]],
  ["SFTP-Browser", [
    [["Enter"], "Ordner öffnen oder Datei auf die andere Seite übertragen (auch Doppelklick)"],
    [["Tab"], "Zwischen Server und Dieser PC wechseln"],
    [["Backspace"], "Eine Ebene hoch, aus der Suche zurück"],
    [["Strg", "F"], "Filtern; Shift+Enter im Feld sucht in allen Unterordnern"],
    [["F7"], "Neuer Ordner"],
    [["F2"], "Umbenennen"],
    [["Entf"], "Löschen"],
    [["F5"], "Aktualisieren"],
    [["Strg", "A"], "Alles markieren"],
  ]],
  ["Ohne Alt-Kürzel", [
    [["Strg", "Shift", "Buchstabe"], "Dieselben Kürzel, wenn die Alt-Kürzel in den Einstellungen aus sind"],
  ]],
];

const keyCombo = (keys) => keys.flatMap((k, i) => [i ? h("span", { class: "plus" }, "+") : null, h("kbd", {}, k)]);

// "log" searches the descriptions; "alt+l" or "strg f" (a modifier in the
// query) looks for exactly these keys, so "l" does not hit every "l".
const plain = (s) => s.toLowerCase().replace(/\s*\+\s*/g, " ");
const words = (q) => plain(q).split(/\s+/).filter(Boolean).map((w) => (w === "ctrl" ? "strg" : w));
const MODIFIERS = new Set(["alt", "strg", "shift"]);

/** True if the row with these keys and this search text matches the query words. */
function matches(q, keys, text) {
  if (!q.some((w) => MODIFIERS.has(w))) return q.every((w) => text.includes(w));
  const parts = keys.flatMap((k) => {
    const range = /^(\d) … (\d)$/.exec(k); // "1 … 9" stands for every digit in it
    if (range) return Array.from({ length: range[2] - range[1] + 1 }, (_, i) => String(+range[1] + i));
    return k.toLowerCase().split(/\s+/);
  });
  return q.every((w) => parts.includes(w));
}

export function showShortcuts() {
  const search = h("input", { type: "search", class: "shortcut-search", placeholder: "Suchen: Befehl oder Taste, z. B. „log“ oder „Alt L“", spellcheck: "false", autofocus: true });
  const groups = SHORTCUTS.map(([title, rows]) => {
    const els = rows.map(([keys, label]) => ({
      keys,
      text: plain(`${keys.join(" ")} ${label} ${title}`),
      el: h("div", { class: "shortcut-row" },
        h("span", { class: "shortcut-keys" }, keyCombo(keys)),
        h("span", { class: "shortcut-label" }, label)),
    }));
    return { els, el: h("section", { class: "shortcut-group" }, h("h3", { class: "shortcut-title" }, title), els.map((r) => r.el)) };
  });
  const none = h("p", { class: "hint shortcut-none" }, "Kein Kürzel passt dazu.");
  const filter = () => {
    const q = words(search.value);
    let shown = 0;
    for (const g of groups) {
      let any = false;
      for (const r of g.els) {
        const hit = matches(q, r.keys, r.text);
        r.el.hidden = !hit;
        any ||= hit;
        if (hit) shown++;
      }
      g.el.hidden = !any;
    }
    none.hidden = shown > 0;
  };
  search.addEventListener("input", filter);
  filter();
  return modal((close) => ({
    head: "Tastenkürzel",
    wide: true,
    body: h("div", { class: "shortcuts-dialog" }, search, h("div", { class: "shortcut-groups" }, groups.map((g) => g.el)), none),
    foot: [h("button", { class: "btn primary", onclick: () => close() }, "Schließen")],
  }));
}
