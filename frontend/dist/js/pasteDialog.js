// Paste dialog: the full text is editable before it is sent, with cleanup
// options and the send mode for this one paste.
import { h, modal, store } from "./ui.js";

const OPTS_KEY = "easytty.pasteOpts";

/** Cleanup applied on "Einfügen" (and for the live line count). */
export function cleanPaste(text, { dropEmpty, dropBang, dropHash, trimRight }) {
  let lines = text.replace(/\r\n?/g, "\n").split("\n");
  if (trimRight) lines = lines.map((l) => l.replace(/[ \t]+$/, ""));
  if (dropBang) lines = lines.filter((l) => !/^\s*!/.test(l));
  if (dropHash) lines = lines.filter((l) => !/^\s*#/.test(l));
  if (dropEmpty) lines = lines.filter((l) => l.trim() !== "");
  while (lines.length && lines[lines.length - 1] === "") lines.pop();
  return lines.join("\n");
}

/** "Bei Fehler anhalten" as last chosen in the dialog (on by default). */
export const stopOnErrorDefault = () => store.get(OPTS_KEY, {}).stopOnError ?? true;

/**
 * Resolves with { text, mode: "paced"|"fast"|"file", delay, stopOnError } or null when cancelled.
 * "file" runs the lines as commands and saves their output to a file.
 * fastDefault: the remote program handles big pastes itself (bracketed paste).
 */
export function editPaste(text, target, { fastDefault = false, delay = 0 } = {}) {
  const saved = store.get(OPTS_KEY, {});
  // Older versions had one switch for both comment kinds (dropComments).
  const opts = {
    dropEmpty: !!saved.dropEmpty,
    dropBang: !!(saved.dropBang ?? saved.dropComments),
    dropHash: !!(saved.dropHash ?? saved.dropComments),
    trimRight: saved.trimRight ?? true,
    stopOnError: saved.stopOnError ?? true,
  };

  const area = h("textarea", { class: "paste-edit", spellcheck: "false", wrap: "off" });
  area.value = text.replace(/\r\n?/g, "\n").replace(/\n$/, "");
  const count = h("span", { class: "hint" });
  const head = h("span", {});
  const update = () => {
    const n = cleanPaste(area.value, opts).split("\n").filter((_, i, a) => a.length > 1 || a[0] !== "").length;
    count.textContent = `${n} Zeile${n === 1 ? "" : "n"} werden gesendet`;
    head.textContent = `${n} Zeile${n === 1 ? "" : "n"} einfügen – Ziel: ${target}`;
  };
  area.addEventListener("input", update);

  // Cleanup switches as toggle chips: on = these lines/characters are left out.
  const chip = (key, label, title) => {
    const b = h("button", { type: "button", class: "chip" + (opts[key] ? " on" : ""), title, "aria-pressed": String(!!opts[key]) }, label);
    b.addEventListener("click", () => {
      opts[key] = !opts[key];
      b.classList.toggle("on", opts[key]);
      b.setAttribute("aria-pressed", String(opts[key]));
      store.set(OPTS_KEY, opts);
      update();
    });
    return b;
  };

  // Send mode as two buttons; the pause only matters line by line.
  let mode = fastDefault ? "fast" : "paced";
  const pause = h("input", { type: "number", min: 0, max: 2000, value: delay, class: "paste-pause", title: "Zusätzliche Pause nach jeder Zeile" });
  const pauseField = h("label", { class: "paste-pause-field" }, "Pause", pause, "ms pro Zeile");
  const stopChip = chip("stopOnError", "Bei Fehler anhalten",
    "Lehnt das Gerät eine Zeile ab (% Invalid input, Error:, syntax error …), wird nichts mehr gesendet und easyTTY zeigt die Stelle");
  const modeBtn = (value, label, title) => h("button", { type: "button", "data-mode": value, title }, label);
  const modes = h("div", { class: "seg" },
    modeBtn("paced", "Zeilenweise mit Echo", "Wartet nach jeder Zeile auf das Gerät – sicher für Switches und Router"),
    modeBtn("fast", "Am Stück", "Alles auf einmal – schnell, für Linux-Shells"),
    modeBtn("file", "Ausgabe in Datei", "Befehle einzeln ausführen, „--More--“ selbst umblättern und Befehle samt Ausgabe als Textdatei im Config-Ordner speichern"));
  const sendBtn = h("button", { class: "btn primary", title: "Enter (neue Zeile: Alt+Enter)" }, "Einfügen");
  const syncMode = () => {
    modes.querySelectorAll("button").forEach((b) => b.classList.toggle("on", b.dataset.mode === mode));
    pauseField.hidden = mode !== "paced";
    stopChip.hidden = mode !== "paced";
    sendBtn.textContent = mode === "file" ? "Ausführen & speichern" : "Einfügen";
  };
  modes.addEventListener("click", (e) => { if (e.target.dataset.mode) { mode = e.target.dataset.mode; syncMode(); } });
  syncMode();
  update();

  return modal((close) => {
    const send = () => close({
      text: cleanPaste(area.value, opts),
      mode,
      delay: Math.max(0, Math.min(2000, parseInt(pause.value, 10) || 0)),
      stopOnError: opts.stopOnError,
    });
    sendBtn.onclick = send;
    return {
      head,
      wide: true,
      sticky: true,
      body: h("div", { class: "paste-dialog" },
        h("p", { class: "hint" }, "Text vor dem Senden prüfen und bei Bedarf direkt hier ändern. Enter sendet, Alt+Enter macht eine neue Zeile."),
        area,
        h("div", { class: "paste-row" },
          h("span", { class: "paste-label" }, "Weglassen"),
          h("div", { class: "paste-chips" },
            chip("trimRight", "Leerzeichen am Zeilenende", "Leerzeichen und Tabs am Ende jeder Zeile entfernen"),
            chip("dropEmpty", "Leere Zeilen", "Leere Zeilen nicht senden"),
            chip("dropBang", "Zeilen mit !", "Zeilen, die mit ! anfangen (Kommentare in Geräte-Configs), nicht senden"),
            chip("dropHash", "Zeilen mit #", "Zeilen, die mit # anfangen (Kommentare in Skripten), nicht senden"))),
        h("div", { class: "paste-row" },
          h("span", { class: "paste-label" }, "Senden"),
          modes, pauseField, stopChip)),
      foot: [
        h("span", { class: "left hint" }, count),
        h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
        sendBtn,
      ],
      // Enter sends (in the text and the pause field), Alt+Enter or Shift+Enter
      // starts a new line, Ctrl+Enter sends from anywhere. Buttons keep their Enter.
      onKey: (e) => {
        if (e.key !== "Enter" || e.isComposing) return;
        if (e.target === area && (e.altKey || e.shiftKey)) {
          e.preventDefault();
          e.stopPropagation();
          area.setRangeText("\n", area.selectionStart, area.selectionEnd, "end");
          area.dispatchEvent(new Event("input"));
          return;
        }
        if (e.ctrlKey || e.target === area || e.target === pause) { e.preventDefault(); e.stopPropagation(); send(); }
      },
    };
  }).then((v) => (v && v.text ? v : null));
}
