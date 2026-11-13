// Session tree in the sidebar: folders, filtering, drag & drop, context menus.
import { h, icon, store } from "./ui.js";

export const DRAG_TYPE = "application/x-easytty";
const EXPANDED_KEY = "easytty.expanded";
const RECENT_COUNT = 5;

const INDENT = 14; // px per level
const TWISTY_MID = 12; // centre of a level-0 twisty (padding 6 + half its 12 px)

/** Padding plus one thin guide line per level, under each parent's twisty. */
function indent(depth) {
  const style = { paddingLeft: 6 + depth * INDENT + "px" };
  if (depth > 0) {
    style.backgroundImage = `repeating-linear-gradient(to right, var(--guide) 0 1px, transparent 1px ${INDENT}px)`;
    style.backgroundPosition = `${TWISTY_MID}px 0`;
    style.backgroundSize = `${(depth - 1) * INDENT + 1}px 100%`;
    style.backgroundRepeat = "no-repeat";
  }
  return style;
}

const ROOT_CLOSED_KEY = "easytty.rootClosed";
const RECENT_CLOSED_KEY = "easytty.recentClosed";

export class SessionTree {
  /**
   * @param {HTMLElement} el
   * @param {object} handlers {connect(s), menuSession(s,x,y), menuFolder(f,x,y), menuRoot(x,y),
   *   moveSession(id, folderId), moveFolder(id, parentId), edit(s), remove(item)}
   */
  constructor(el, filterInput, handlers) {
    this.el = el;
    this.filterInput = filterInput;
    this.handlers = handlers;
    this.data = { folders: [], sessions: [] };
    const saved = store.get(EXPANDED_KEY, []);
    this.expanded = new Set(Array.isArray(saved) ? saved : []);
    this.rootClosed = store.get(ROOT_CLOSED_KEY) === true;
    this.recentClosed = store.get(RECENT_CLOSED_KEY) === true;
    this.selected = null; // {type, id}
    this.marked = new Set(); // extra sessions marked with Ctrl/Shift+click (multi-select)
    this.live = new Set(); // session ids with open tabs
    this.filter = "";
    filterInput.addEventListener("input", () => { this.filter = filterInput.value.trim().toLowerCase(); this.render(); });
    filterInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { const first = this.visibleSessions()[0]; if (first) this.open(e, [first]); }
      if (e.key === "Escape") { filterInput.value = ""; this.filter = ""; this.render(); }
      if (e.key === "ArrowDown") { e.preventDefault(); this.el.focus(); this.moveSelection(1); }
    });
    el.addEventListener("keydown", (e) => this.onKey(e));
    el.addEventListener("contextmenu", (e) => {
      if (e.target === el || e.target.classList.contains("tree-empty")) { e.preventDefault(); handlers.menuRoot(e.clientX, e.clientY); }
    });
    this.bindRootDrop();
  }

  set(data) {
    this.data = { folders: data.folders || [], sessions: data.sessions || [] };
    for (const id of this.marked) if (!this.session(id)) this.marked.delete(id); // deleted meanwhile
    this.render();
  }

  setLive(ids) {
    const next = new Set(ids);
    if (this.live && next.size === this.live.size && [...next].every((id) => this.live.has(id))) return; // a drag in progress survives
    this.live = next;
    this.render();
  }

  session(id) { return this.data.sessions.find((s) => s.id === id); }
  folder(id) { return this.data.folders.find((f) => f.id === id); }

  recent() {
    return this.data.sessions.filter((s) => s.lastUsed).sort((a, b) => b.lastUsed - a.lastUsed).slice(0, RECENT_COUNT);
  }

  matches(s) {
    if (!this.filter) return true;
    return [s.name, s.host, s.user, s.serialPort, s.notes].some((v) => (v || "").toLowerCase().includes(this.filter));
  }

  visibleSessions() {
    return [...this.el.querySelectorAll(".node[data-type=session]")].map((n) => this.session(n.dataset.id)).filter(Boolean);
  }

  saveExpanded() { store.set(EXPANDED_KEY, [...this.expanded]); }

  toggle(id, open) {
    const isOpen = open ?? !this.expanded.has(id);
    if (isOpen) this.expanded.add(id); else this.expanded.delete(id);
    this.saveExpanded();
    this.render();
  }

  render() {
    const { folders, sessions } = this.data;
    const kidsF = new Map();
    const kidsS = new Map();
    for (const f of folders) (kidsF.get(f.parentId) || kidsF.set(f.parentId, []).get(f.parentId)).push(f);
    for (const s of sessions) (kidsS.get(s.folderId) || kidsS.set(s.folderId, []).get(s.folderId)).push(s);
    const byName = (a, b) => a.name.localeCompare(b.name, "de", { numeric: true });

    // With a filter, a folder is shown if it contains a match somewhere below.
    const hasMatch = new Map();
    const check = (fid) => {
      if (hasMatch.has(fid)) return hasMatch.get(fid);
      const r = (kidsS.get(fid) || []).some((s) => this.matches(s)) || (kidsF.get(fid) || []).some((f) => check(f.id));
      hasMatch.set(fid, r);
      return r;
    };

    const frag = document.createDocumentFragment();
    if (!this.filter) {
      const rec = this.recent();
      if (rec.length) {
        const open = !this.recentClosed;
        const head = h("button", {
          type: "button", class: "tree-section toggle", "aria-expanded": String(open),
          title: open ? "Zuletzt verwendet einklappen" : "Zuletzt verwendet ausklappen",
        }, h("span", { class: "twisty" }, open ? "▾" : "▸"), "Zuletzt verwendet", open ? null : h("span", { class: "count" }, String(rec.length)));
        head.addEventListener("click", () => {
          this.recentClosed = open;
          store.set(RECENT_CLOSED_KEY, this.recentClosed);
          this.render();
        });
        frag.append(head);
        if (open) rec.forEach((s) => frag.append(this.sessionNode(s, 0, true)));
      }
    }
    const rootOpen = !!this.filter || !this.rootClosed;
    frag.append(this.rootNode(sessions.length, rootOpen));
    const walk = (parent, depth) => {
      for (const f of (kidsF.get(parent) || []).sort(byName)) {
        if (this.filter && !check(f.id)) continue;
        const open = this.filter ? true : this.expanded.has(f.id);
        frag.append(this.folderNode(f, depth, open));
        if (open) walk(f.id, depth + 1);
      }
      for (const s of (kidsS.get(parent) || []).sort(byName)) {
        if (this.matches(s)) frag.append(this.sessionNode(s, depth, false));
      }
    };
    if (rootOpen) walk("", 1);

    this.el.replaceChildren(frag);
    if (!sessions.length && !folders.length) {
      this.el.append(h("div", { class: "tree-empty" },
        "Noch keine Sessions.", h("br"), "„+ Session“ oben oder", h("br"), "Import in den Einstellungen."));
    } else if (this.filter && !this.el.querySelector(".node")) {
      this.el.append(h("div", { class: "tree-empty" }, "Keine Treffer."));
    }
  }

  isSelected(type, id) {
    if (type === "session" && this.marked.has(id)) return true;
    return this.selected && this.selected.type === type && this.selected.id === id;
  }

  /** Sessions the next action applies to: all marked ones, else the selected one. */
  markedSessions() {
    const ids = new Set(this.marked);
    if (this.selected?.type === "session") ids.add(this.selected.id);
    return [...ids].map((id) => this.session(id)).filter(Boolean);
  }

  /** Enter: own tab; Shift = into the open layout; Ctrl = as a new block. */
  open(e, sessions) {
    if (e.shiftKey) this.handlers.joinLayout(sessions);
    else if (e.ctrlKey) this.handlers.newBlock(sessions);
    else this.handlers.connect(sessions[0]);
  }

  /** Click on a session: plain = select, Ctrl = add/remove, Shift = range. */
  clickSession(e, id) {
    if (e.ctrlKey || e.metaKey) {
      if (this.selected?.type === "session") this.marked.add(this.selected.id);
      if (this.marked.has(id)) this.marked.delete(id); else this.marked.add(id);
      // Un-marking the selected session must really drop it from the selection.
      const last = this.marked.has(id) ? id : [...this.marked].pop();
      this.selected = last ? { type: "session", id: last } : null;
      this.paintMarks();
      return;
    }
    if (e.shiftKey && this.selected?.type === "session") {
      const nodes = [...this.el.querySelectorAll(".node[data-type=session]")].map((n) => n.dataset.id);
      const a = nodes.indexOf(this.selected.id), b = nodes.indexOf(id);
      if (a >= 0 && b >= 0) {
        for (const x of nodes.slice(Math.min(a, b), Math.max(a, b) + 1)) this.marked.add(x);
        this.paintMarks();
        return;
      }
    }
    this.select("session", id);
  }

  /** Ctrl+A: mark every visible session. */
  markAll() {
    for (const n of this.el.querySelectorAll(".node[data-type=session]")) this.marked.add(n.dataset.id);
    this.paintMarks();
  }

  paintMarks() {
    for (const n of this.el.querySelectorAll(".node[data-type=session], .node[data-type=folder]")) {
      n.classList.toggle("selected", this.isSelected(n.dataset.type, n.dataset.id));
    }
  }

  /** "Alle Sessions": the global level above all folders. */
  rootNode(count, open) {
    const n = h("div", {
      class: "node root" + (this.isSelected("root", "") ? " selected" : ""),
      "data-type": "root", "data-id": "", style: indent(0), title: "Alle Sessions – Rechtsklick: für alle ändern, neue Session, Import/Export",
    },
    h("span", { class: "twisty" }, open ? "▾" : "▸"),
    h("span", { class: "ico" }, "🗂"),
    h("span", { class: "label" }, "Alle Sessions"),
    h("span", { class: "sub" }, String(count)));
    n.addEventListener("click", () => {
      this.select("root", "");
      if (this.filter) return;
      this.rootClosed = !this.rootClosed;
      store.set(ROOT_CLOSED_KEY, this.rootClosed);
      this.render();
    });
    n.addEventListener("contextmenu", (e) => { e.preventDefault(); e.stopPropagation(); this.select("root", ""); this.handlers.menuRoot(e.clientX, e.clientY); });
    this.bindDrop(n, ""); // dropping on it = top level
    return n;
  }

  folderNode(f, depth, open) {
    const n = h("div", {
      class: "node" + (this.isSelected("folder", f.id) ? " selected" : ""),
      "data-type": "folder", "data-id": f.id, draggable: "true",
      style: indent(depth),
    },
    h("span", { class: "twisty" }, open ? "▾" : "▸"),
    h("span", { class: "ico" }, open ? "📂" : "📁"),
    h("span", { class: "label" }, f.name),
    f.layout ? h("button", {
      class: "play", title: "Fenster-Layout öffnen (alle Sessions in ihren Fenstern)",
      onclick: (e) => { e.stopPropagation(); this.handlers.openLayout(f); },
    }, "▶ ▦") : null);
    n.addEventListener("click", () => { this.select("folder", f.id); this.toggle(f.id); });
    n.addEventListener("contextmenu", (e) => { e.preventDefault(); this.select("folder", f.id); this.handlers.menuFolder(f, e.clientX, e.clientY); });
    this.bindDrag(n, "folder", f.id);
    this.bindDrop(n, f.id);
    return n;
  }

  sessionNode(s, depth, inRecent) {
    const defPort = s.protocol === "rdp" ? 3389 : 22;
    const sub = s.protocol === "serial" ? `${s.serialPort} ${s.baud}` : (s.user ? s.user + "@" : "") + s.host + (s.port !== defPort ? ":" + s.port : "");
    // Jump host and port forwards: one small mark, the details in the tooltip.
    const route = [
      s.jumpId ? `Über Sprung-Host: ${this.session(s.jumpId)?.name || "?"}` : "",
      ...(s.forwards || []).map((f) => `Weiterleitung: 127.0.0.1:${f.listen} → ${f.host}:${f.port}`),
    ].filter(Boolean);
    const n = h("div", {
      class: "node" + (this.isSelected("session", s.id) && !inRecent ? " selected" : ""),
      "data-type": inRecent ? "recent" : "session", "data-id": s.id, draggable: inRecent ? "false" : "true",
      style: indent(depth),
      // Names are often cut off in the tree.
      title: [s.name, ...route, s.notes].filter(Boolean).join("\n"),
    },
    h("span", { class: "twisty" }),
    s.color ? h("span", { class: "dot", style: { background: s.color } }) : h("span", { class: "ico", title: s.protocol === "rdp" ? "Remotedesktop" : null }, s.protocol === "serial" ? "⏚" : s.protocol === "rdp" ? icon("monitor") : "❯"),
    h("span", { class: "label" }, s.name),
    route.length ? h("span", { class: "route", title: route.join("\n") }, "↪") : null,
    this.live.has(s.id) ? h("span", { class: "live", title: "verbunden" }) : null,
    h("span", { class: "sub" }, sub));
    n.addEventListener("click", (e) => { if (inRecent) this.select("session", s.id); else this.clickSession(e, s.id); });
    n.addEventListener("dblclick", () => this.handlers.connect(s));
    n.addEventListener("contextmenu", (e) => {
      e.preventDefault();
      const many = this.markedSessions();
      if (!inRecent && many.length > 1 && many.some((x) => x.id === s.id)) { this.handlers.menuSessions(many, e.clientX, e.clientY); return; }
      this.select("session", s.id);
      this.handlers.menuSession(s, e.clientX, e.clientY);
    });
    if (!inRecent) {
      this.bindDrag(n, "session", s.id);
      this.bindDrop(n, s.folderId); // dropping on a session = into its folder
    }
    return n;
  }

  clearSelection() {
    this.selected = null;
    this.marked.clear();
    this.el.querySelectorAll(".node.selected").forEach((n) => n.classList.remove("selected"));
  }

  select(type, id) {
    this.selected = { type, id };
    this.marked.clear();
    this.el.querySelectorAll(".node.selected").forEach((n) => n.classList.remove("selected"));
    const n = this.el.querySelector(`.node[data-type=${type}][data-id="${id}"]`);
    if (n) { n.classList.add("selected"); n.scrollIntoView({ block: "nearest" }); }
  }

  moveSelection(delta) {
    const nodes = [...this.el.querySelectorAll(".node[data-type=folder], .node[data-type=session]")];
    if (!nodes.length) return;
    const sel = this.selected;
    let i = nodes.findIndex((n) => n.dataset.type === sel?.type && n.dataset.id === sel?.id);
    i = Math.max(0, Math.min(nodes.length - 1, i < 0 ? 0 : i + delta));
    const { type, id } = nodes[i].dataset;
    if (!this.marked.size) { this.select(type, id); return; }
    this.selected = { type, id }; // marks made with the space bar stay while the cursor moves
    this.paintMarks();
    nodes[i].scrollIntoView({ block: "nearest" });
  }

  /** Space bar: marks the session under the cursor, or takes the mark off. */
  toggleMark(id) {
    if (this.marked.has(id)) this.marked.delete(id); else this.marked.add(id);
    this.paintMarks();
  }

  onKey(e) {
    if (e.key === "Escape") { e.preventDefault(); this.handlers.leave?.(); return; }
    const sel = this.selected;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); this.moveSelection(e.key === "ArrowDown" ? 1 : -1); return; }
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "a") { e.preventDefault(); this.markAll(); return; }
    if (!sel) return;
    const item = sel.type === "session" ? this.session(sel.id) : this.folder(sel.id);
    if (!item) return;
    if (e.key === "Enter") {
      e.preventDefault();
      if (sel.type === "session") this.open(e, e.shiftKey || e.ctrlKey ? this.markedSessions() : [item]);
      else this.toggle(item.id);
    } else if (e.key === " " && sel.type === "session") {
      e.preventDefault();
      this.toggleMark(sel.id);
    } else if (e.key === "ArrowRight" && sel.type === "folder") { this.toggle(item.id, true); }
    else if (e.key === "ArrowLeft" && sel.type === "folder") { this.toggle(item.id, false); }
    else if (e.key === "F2") { e.preventDefault(); this.handlers.edit(sel.type, item); }
    else if (e.key === "Delete") {
      e.preventDefault();
      const many = this.markedSessions();
      if (many.length > 1) this.handlers.removeSessions(many); else this.handlers.remove(sel.type, item);
    }
  }

  bindDrag(n, type, id) {
    n.addEventListener("dragstart", (e) => {
      e.dataTransfer.setData(DRAG_TYPE, JSON.stringify({ type, id }));
      e.dataTransfer.effectAllowed = "move";
    });
  }

  bindDrop(n, folderId) {
    n.addEventListener("dragover", (e) => {
      if (!e.dataTransfer.types.includes(DRAG_TYPE)) return;
      e.preventDefault();
      e.stopPropagation();
      n.classList.add("drop");
    });
    n.addEventListener("dragleave", () => n.classList.remove("drop"));
    n.addEventListener("drop", (e) => {
      n.classList.remove("drop");
      const raw = e.dataTransfer.getData(DRAG_TYPE);
      if (!raw) return;
      e.preventDefault();
      e.stopPropagation();
      try { this.dropOn(JSON.parse(raw), folderId); } catch { /* foreign drag data */ }
    });
  }

  bindRootDrop() {
    this.el.addEventListener("dragover", (e) => {
      if (!e.dataTransfer.types.includes(DRAG_TYPE)) return;
      e.preventDefault();
      this.el.classList.add("drop-root");
    });
    this.el.addEventListener("dragleave", (e) => { if (e.target === this.el) this.el.classList.remove("drop-root"); });
    this.el.addEventListener("drop", (e) => {
      this.el.classList.remove("drop-root");
      const raw = e.dataTransfer.getData(DRAG_TYPE);
      if (!raw) return;
      e.preventDefault();
      try { this.dropOn(JSON.parse(raw), ""); } catch { /* foreign drag data */ }
    });
    addEventListener("dragend", () => this.el.classList.remove("drop-root"));
  }

  dropOn(item, folderId) {
    if (item.type === "session") this.handlers.moveSession(item.id, folderId);
    else if (item.id !== folderId) {
      if (folderId) { this.expanded.add(folderId); this.saveExpanded(); }
      this.handlers.moveFolder(item.id, folderId);
    }
  }
}
