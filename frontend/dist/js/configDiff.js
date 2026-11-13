// Compare two saved configs (or command outputs) from the config folder:
// what was removed, what was added.
import { api, errMsg } from "./api.js";
import { fmtDate, h, modal, toast } from "./ui.js";

const CONTEXT = 3; // unchanged lines shown around a change

/** Rows to show: every change with CONTEXT lines around it, gaps as {gap: n}. */
export function withContext(lines, context = CONTEXT) {
  const keep = new Array(lines.length).fill(false);
  lines.forEach((l, i) => {
    if (l.kind === " ") return;
    for (let k = Math.max(0, i - context); k <= Math.min(lines.length - 1, i + context); k++) keep[k] = true;
  });
  const out = [];
  let gap = 0;
  lines.forEach((l, i) => {
    if (keep[i]) {
      if (gap) { out.push({ gap }); gap = 0; }
      out.push(l);
    } else gap++;
  });
  if (gap) out.push({ gap });
  return out;
}

/** The file to compare `file` with: the next older one of the same device, else the next older one. */
export function previousOf(file, files) {
  const older = files.filter((f) => f.path !== file.path && f.modTime <= file.modTime);
  return older.find((f) => f.device === file.device) || older[0] || files.find((f) => f.path !== file.path) || null;
}

/** Opens the dialog. newPath (optional) preselects the newer file. */
export async function openConfigDiff(newPath = "") {
  let files;
  try { files = await api.ConfigFiles(); } catch (e) { toast(errMsg(e), "err"); return; }
  if (files.length < 2) {
    toast("Zum Vergleichen braucht es zwei gesicherte Dateien im Config-Ordner.");
    return;
  }
  const newest = files.find((f) => f.path === newPath) || files[0];
  const option = (f) => h("option", { value: f.path }, `${f.name} · ${fmtDate(f.modTime)}`);
  const newSel = h("select", {}, files.map(option));
  const oldSel = h("select", {}, files.map(option));
  newSel.value = newest.path;
  oldSel.value = previousOf(newest, files).path;

  let ignoreVolatile = true;
  let onlyChanges = true;
  let last = null;
  const summary = h("span", { class: "hint" });
  const view = h("div", { class: "diff-view", tabindex: "0" });

  const paint = () => {
    if (!last) return;
    const rows = onlyChanges ? withContext(last.lines) : last.lines;
    summary.textContent = last.removed || last.added
      ? `${last.removed} entfernt, ${last.added} hinzugefügt`
      : "Keine Unterschiede";
    if (!last.removed && !last.added && onlyChanges) {
      view.replaceChildren(h("div", { class: "diff-empty" }, "Beide Dateien sind gleich."));
      return;
    }
    view.replaceChildren(...rows.map((r) => (r.gap
      ? h("div", { class: "diff-gap" }, `… ${r.gap} gleiche Zeile${r.gap === 1 ? "" : "n"} …`)
      : h("div", { class: "diff-line " + (r.kind === "-" ? "del" : r.kind === "+" ? "add" : "") },
        h("span", { class: "no" }, r.oldNo || ""), h("span", { class: "no" }, r.newNo || ""),
        h("span", { class: "sign" }, r.kind), h("span", { class: "text" }, r.text)))));
  };

  const load = async () => {
    if (oldSel.value === newSel.value) {
      last = null;
      summary.textContent = "";
      view.replaceChildren(h("div", { class: "diff-empty" }, "Zweimal dieselbe Datei gewählt."));
      return;
    }
    try {
      last = await api.DiffConfigs(oldSel.value, newSel.value, ignoreVolatile);
      paint();
    } catch (e) {
      last = null;
      view.replaceChildren(h("div", { class: "diff-empty" }, errMsg(e)));
    }
  };

  const chip = (label, title, get, set) => {
    const b = h("button", { type: "button", class: "chip" + (get() ? " on" : ""), title }, label);
    b.addEventListener("click", () => { set(!get()); b.classList.toggle("on", get()); });
    return b;
  };
  oldSel.addEventListener("change", load);
  newSel.addEventListener("change", load);
  await load();

  return modal((close) => ({
    head: "Configs vergleichen",
    wide: true,
    body: h("div", { class: "diff-dialog" },
      h("div", { class: "grid" },
        h("label", { class: "field" }, h("span", {}, "Alt"), oldSel),
        h("label", { class: "field" }, h("span", {}, "Neu"), newSel)),
      h("div", { class: "paste-row" },
        chip("Nur Änderungen", "Gleiche Zeilen bis auf drei davor und danach ausblenden", () => onlyChanges, (v) => { onlyChanges = v; paint(); }),
        chip("Zeitstempel ignorieren", "Zeilen weglassen, die sich bei jeder Sicherung ändern: Größe, Änderungszeit, NTP-Drift", () => ignoreVolatile, (v) => { ignoreVolatile = v; load(); })),
      view),
    foot: [
      h("span", { class: "left hint" }, summary),
      h("button", { class: "btn", onclick: () => api.OpenConfigDir().catch((e) => toast(errMsg(e), "err")) }, "Ordner öffnen"),
      h("button", { class: "btn primary", onclick: () => close() }, "Schließen"),
    ],
  }));
}
