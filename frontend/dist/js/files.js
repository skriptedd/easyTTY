// "Logs & Dateien" (Alt+L): session logs, saved running-configs and snippet
// output in one searchable list, newest first. Enter opens a file,
// Shift+Enter shows it in Explorer.
import { api, errMsg } from "./api.js";
import { openConfigDiff } from "./configDiff.js";
import { fmtDate, fmtSize, h, modal, toast } from "./ui.js";

const KINDS = [["", "Alle"], ["log", "Logs"], ["config", "Configs"], ["output", "Ausgaben"]];
const KIND_LABEL = { log: "Log", config: "Config", output: "Ausgabe" };
const MAX_ROWS = 300; // more rows only slow the dialog down; the search narrows it

const fail = (e) => toast(errMsg(e), "err");
export const openFile = (path) => api.OpenFile(path).catch(fail);
export const revealFile = (path) => api.RevealLocal(path).catch(fail);

/** Opens the list; select: a path to put the cursor on (e.g. this tab's log). */
export async function showFiles({ select = "" } = {}) {
  let files;
  try { files = await api.FileList(); } catch (e) { fail(e); return; }
  let kind = "";
  let shown = [];
  let cursor = Math.max(0, files.findIndex((f) => f.path === select));
  let cursorPath = files[cursor]?.path || "";

  const search = h("input", { type: "search", class: "files-search", placeholder: "Suchen: Gerät, Datum, Befehl …", spellcheck: "false", autofocus: true });
  const list = h("div", { class: "files-list", role: "listbox" });
  const count = h("span", { class: "muted" });
  const kindButtons = KINDS.map(([k, label]) => h("button", {
    class: "btn small" + (k === kind ? " active" : ""), type: "button",
    onclick: () => {
      kind = k;
      kindButtons.forEach((b, i) => b.classList.toggle("active", KINDS[i][0] === k));
      render();
      search.focus();
    },
  }, label));

  const row = (f, i) => h("div", {
    class: "files-row" + (f.path === select ? " current" : ""),
    role: "option", title: f.path,
    onclick: () => { cursor = i; cursorPath = f.path; paint(); search.focus(); }, // keys keep working
    ondblclick: () => openFile(f.path),
  },
  h("div", { class: "files-main" },
    h("span", { class: "files-name" }, f.name,
      f.path === select ? h("span", { class: "files-badge" }, "diese Sitzung") : null),
    h("span", { class: "files-meta" }, [KIND_LABEL[f.kind] || f.kind, f.folder, fmtDate(f.time), fmtSize(f.size)].filter(Boolean).join(" · "))),
  h("div", { class: "files-actions" },
    h("button", { class: "btn small", type: "button", title: "Mit dem Standardprogramm öffnen (Enter)", onclick: (e) => { e.stopPropagation(); openFile(f.path); } }, "Öffnen"),
    h("button", { class: "btn small", type: "button", title: "Im Explorer zeigen (Shift+Enter)", onclick: (e) => { e.stopPropagation(); revealFile(f.path); } }, "Ordner"),
    f.kind !== "log" ? h("button", { class: "btn small", type: "button", title: "Mit einer älteren Datei vergleichen", onclick: (e) => { e.stopPropagation(); openConfigDiff(f.path); } }, "Vergleichen") : null));

  const paint = () => {
    [...list.children].forEach((el, i) => el.classList.toggle("cursor", i === cursor && !!shown.length));
    list.children[cursor]?.scrollIntoView({ block: "nearest" });
  };

  const render = () => {
    const words = search.value.toLowerCase().split(/\s+/).filter(Boolean);
    const all = files.filter((f) => (!kind || f.kind === kind)
      && words.every((w) => `${f.name} ${f.folder} ${KIND_LABEL[f.kind]}`.toLowerCase().includes(w)));
    shown = all.slice(0, MAX_ROWS);
    cursor = Math.max(0, shown.findIndex((f) => f.path === cursorPath));
    cursorPath = shown[cursor]?.path || "";
    list.replaceChildren(...shown.map(row));
    if (!shown.length) {
      list.append(h("p", { class: "hint files-empty" }, files.length
        ? "Keine Datei passt zur Suche."
        : "Noch keine Logs oder gespeicherten Dateien. Logs entstehen beim Verbinden (Einstellungen → Session-Logging), Configs über „Running-Config sichern“, Ausgaben über Schnipsel mit „Ausgabe in Datei“."));
    }
    count.textContent = all.length > shown.length ? `${shown.length} von ${all.length}, die Suche grenzt ein` : `${all.length} Dateien`;
    paint();
  };

  search.addEventListener("input", render);
  search.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!shown.length) return;
      cursor = Math.max(0, Math.min(shown.length - 1, cursor + (e.key === "ArrowDown" ? 1 : -1)));
      cursorPath = shown[cursor].path;
      paint();
    } else if (e.key === "Enter" && shown[cursor]) {
      e.preventDefault();
      (e.shiftKey ? revealFile : openFile)(shown[cursor].path);
    }
  });
  render();

  return modal((close) => ({
    head: "Logs & Dateien",
    wide: true,
    body: h("div", { class: "files-dialog" },
      h("div", { class: "files-bar" }, search, h("div", { class: "files-kinds" }, kindButtons)),
      list,
      h("div", { class: "files-foot" }, count, h("span", { class: "hint" }, "Enter öffnet · Shift+Enter zeigt im Explorer · Doppelklick öffnet"))),
    foot: [
      h("button", { class: "btn", onclick: () => api.OpenLogDir().catch(fail) }, "Log-Ordner"),
      h("button", { class: "btn", onclick: () => api.OpenConfigDir().catch(fail) }, "Config-Ordner"),
      h("button", { class: "btn primary", onclick: () => close() }, "Schließen"),
    ],
  }));
}
