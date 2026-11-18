// Putting sessions into folders: a new folder asks right away which sessions
// go in, a folder can take more ("Sessions hinzufügen…"), and marked
// sessions can be copied or moved into any folder.
import { api, errMsg } from "./api.js";
import { h, modal, store, toast } from "./ui.js";
import { folderPath, sessionPicker } from "./sessionPicker.js";

const MODE_KEY = "easytty.folderSessionMode";
const MODES = [
  ["copy", "Kopieren", "Die Sessions bleiben auch an ihrem alten Platz. Gespeicherte Passwörter werden mitkopiert."],
  ["move", "Verschieben", "Die Sessions wandern in den Ordner."],
];

const plural = (n) => `${n} Session${n === 1 ? "" : "s"}`;

/** Copy or move, remembered for next time. */
function modeChoice() {
  let mode = store.get(MODE_KEY, "copy") === "move" ? "move" : "copy";
  const hint = h("p", { class: "hint" });
  const show = () => { hint.textContent = MODES.find(([v]) => v === mode)[2]; };
  const name = `mode-${Math.random().toString(36).slice(2)}`;
  const options = MODES.map(([value, label]) => {
    const r = h("input", { type: "radio", name, value, checked: value === mode });
    r.addEventListener("change", () => { if (r.checked) { mode = value; show(); } });
    return h("label", { class: "check" }, r, label);
  });
  show();
  return {
    el: h("div", { class: "mode-choice" }, h("div", { class: "row" }, options), hint),
    value: () => { store.set(MODE_KEY, mode); return mode; },
  };
}

/**
 * "Neuer Ordner": name plus the sessions that go in (optional).
 * Resolves with {name, ids, mode} or null.
 */
export function askNewFolder(tree, parentId = "", preselect = []) {
  const name = h("input", { autofocus: true, spellcheck: "false", placeholder: "z. B. Kunde Müller" });
  const picker = sessionPicker(tree, preselect, { rdp: true });
  const mode = modeChoice();
  const where = parentId ? `in „${folderPath(tree.folders, parentId)}“` : "auf der obersten Ebene";
  return modal((close) => {
    const submit = () => {
      if (!name.value.trim()) { name.focus(); toast("Der Ordner braucht einen Namen.", "err"); return; }
      close({ name: name.value.trim(), ids: picker.value(), mode: mode.value() });
    };
    return {
      head: "Neuer Ordner",
      wide: true,
      body: h("div", { class: "grid" },
        h("label", { class: "field full" }, h("span", {}, `Name (${where})`), name),
        h("div", { class: "field full" }, h("span", {}, "Sessions für den Ordner (optional)"), picker.el),
        h("div", { class: "field full" }, mode.el)),
      foot: [
        h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
        h("button", { class: "btn primary", onclick: submit }, "Anlegen"),
      ],
      onKey: (e) => { if (e.key === "Enter" && e.target === name) { e.preventDefault(); submit(); } },
    };
  }).then((v) => v || null);
}

/** "Sessions hinzufügen…" on a folder. Resolves with {ids, mode} or null. */
export function askAddSessions(tree, folder) {
  const picker = sessionPicker(tree, [], { rdp: true, skip: folder.id });
  const mode = modeChoice();
  return modal((close) => {
    const submit = () => {
      const ids = picker.value();
      if (!ids.length) { toast("Keine Session ausgewählt."); return; }
      close({ ids, mode: mode.value() });
    };
    return {
      head: `Sessions in „${folder.name}“`,
      wide: true,
      body: h("div", { class: "grid" },
        h("div", { class: "field full" }, h("span", {}, "Welche Sessions sollen hinein?"), picker.el),
        h("div", { class: "field full" }, mode.el)),
      foot: [
        h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
        h("button", { class: "btn primary", onclick: submit }, "Hinzufügen"),
      ],
    };
  }).then((v) => v || null);
}

/**
 * Target folder for marked sessions. Resolves with {folderId, mode} or null;
 * folderId "" is the top level.
 */
export function askTargetFolder(tree, sessions) {
  const options = [["", "Oberste Ebene"], ...tree.folders.map((f) => [f.id, folderPath(tree.folders, f.id)])
    .sort((a, b) => a[1].localeCompare(b[1]))];
  const list = h("select", { size: Math.min(Math.max(options.length, 4), 12), class: "folder-list" },
    options.map(([id, label]) => h("option", { value: id }, label)));
  // Preselect the first folder the sessions are not in yet.
  const here = new Set(sessions.map((s) => s.folderId || ""));
  list.value = (options.find(([id]) => !here.has(id)) || options[0])[0];
  const mode = modeChoice();
  const names = sessions.slice(0, 6).map((s) => s.name).join(", ") + (sessions.length > 6 ? ` … und ${sessions.length - 6} weitere` : "");
  return modal((close) => {
    const submit = () => close({ folderId: list.value, mode: mode.value() });
    list.addEventListener("dblclick", submit);
    return {
      head: `${plural(sessions.length)} in Ordner`,
      body: h("div", { class: "grid" },
        h("p", { class: "muted full" }, names),
        h("label", { class: "field full" }, h("span", {}, "Ziel-Ordner"), list),
        h("div", { class: "field full" }, mode.el)),
      foot: [
        h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
        h("button", { class: "btn primary", onclick: submit }, "OK"),
      ],
      onKey: (e) => { if (e.key === "Enter" && e.target === list) { e.preventDefault(); submit(); } },
    };
  }).then((v) => v || null);
}

/** Copies or moves sessions into a folder and says what happened. */
export async function putSessions(ids, folderId, mode) {
  if (!ids.length) return false;
  try {
    if (mode === "move") await api.MoveSessions(ids, folderId);
    else await api.CopySessions(ids, folderId);
    toast(`${plural(ids.length)} ${mode === "move" ? "verschoben" : "kopiert"}.`, "ok");
    return true;
  } catch (e) {
    toast(errMsg(e), "err");
    return false;
  }
}
