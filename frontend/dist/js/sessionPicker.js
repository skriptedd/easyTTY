// Session picker grouped by folder, with a search box (tasks, folders).
import { h } from "./ui.js";

/** Folder path for display, "Oberste Ebene" for the root. */
export function folderPath(folders, id) {
  const byId = new Map(folders.map((f) => [f.id, f]));
  const out = [];
  for (let f = byId.get(id), g = 0; f && g < 32; f = byId.get(f.parentId), g++) out.unshift(f.name);
  return out.join(" › ") || "Oberste Ebene";
}

/**
 * Checkbox list of sessions; ticks survive filtering.
 * Options: rdp (list RDP sessions too), skip (folder id whose sessions are left out),
 * loginWarn (mark SSH sessions without stored login).
 */
export function sessionPicker(tree, selected, { rdp = false, skip = null, loginWarn = false } = {}) {
  // Only sessions the list shows (deleted or hidden ones would stay ticked invisibly).
  const shown = tree.sessions.filter((x) => (rdp || x.protocol !== "rdp") && (skip === null || x.folderId !== skip));
  const listed = new Set(shown.map((x) => x.id));
  const chosen = new Set(selected.filter((id) => listed.has(id)));
  const groups = new Map();
  for (const s of [...shown].sort((a, b) => a.name.localeCompare(b.name))) {
    const key = folderPath(tree.folders, s.folderId);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(s);
  }
  const counter = h("span", { class: "muted task-count" });
  const count = () => { counter.textContent = chosen.size ? `${chosen.size} ausgewählt` : "keine ausgewählt"; };
  const rows = []; // {el, text, group}
  const groupEls = [...groups.entries()].sort().map(([folder, list]) => {
    const head = h("div", { class: "task-folder" }, folder);
    const items = list.map((s) => {
      const cb = h("input", { type: "checkbox", checked: chosen.has(s.id) });
      cb.addEventListener("change", () => { if (cb.checked) chosen.add(s.id); else chosen.delete(s.id); count(); });
      const how = s.protocol === "serial" ? s.serialPort : `${s.user ? s.user + "@" : ""}${s.host}`;
      const warn = loginWarn && s.protocol === "ssh" && (!s.user || (s.auth === "password" && !s.hasPassword)) ? " ⚠ ohne gespeicherte Anmeldung" : "";
      const el = h("label", { class: "check" }, cb, h("span", {}, s.name), h("span", { class: "muted" }, " " + how + warn));
      rows.push({ el, cb, text: [s.name, s.host, s.user, s.serialPort, folder].join(" ").toLowerCase() });
      return el;
    });
    const group = h("div", {}, head, items);
    group.rows = rows.slice(-items.length);
    return group;
  });
  const list = h("div", { class: "task-sessions" }, groupEls);
  const empty = h("div", { class: "muted task-empty" }, "Keine Session passt zur Suche.");
  list.append(empty);
  const search = h("input", { class: "task-search", type: "search", placeholder: "Sessions suchen (Name, Host, Benutzer, Ordner) …", spellcheck: "false" });
  const filter = () => {
    const words = search.value.toLowerCase().split(/\s+/).filter(Boolean);
    let shown = 0;
    for (const g of groupEls) {
      let any = false;
      for (const r of g.rows) {
        const hit = words.every((w) => r.text.includes(w));
        r.el.hidden = !hit;
        any ||= hit;
        if (hit) shown++;
      }
      g.hidden = !any;
    }
    empty.hidden = shown > 0;
  };
  search.addEventListener("input", filter);
  // Enter in the search ticks the only hit (quick: type, Enter).
  search.addEventListener("keydown", (e) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    e.stopPropagation();
    const hits = rows.filter((r) => !r.el.hidden);
    if (hits.length === 1) { hits[0].cb.checked = !hits[0].cb.checked; hits[0].cb.dispatchEvent(new Event("change")); }
  });
  filter();
  count();
  const el = h("div", { class: "task-picker" }, h("div", { class: "row" }, search, counter), list);
  // A preselected session is scrolled into view.
  setTimeout(() => rows.find((r) => r.cb.checked)?.el.scrollIntoView({ block: "nearest" }), 0);
  return { el, value: () => [...chosen] };
}
