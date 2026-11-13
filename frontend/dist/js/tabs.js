// The tab bar and the workspace: opens, arranges, switches and closes tabs.
import { events } from "./api.js";
import { contextMenu, h, overlayOpen, toast } from "./ui.js";
import { barEntries, blockTab, syncBlocks } from "./tabBlocks.js";
import { gridStyle, TEMPLATES, templateFor } from "./layouts.js";
import { Tab, TAB_DRAG } from "./tab.js";
import { RdpTab } from "./rdpTab.js";

/** Drag type of a tab from the tab bar (dropped into the workspace = into the view). */
export { TAB_DRAG };

// A full view grows one step when another session joins it.
const GROW = { "1": "2v", "2v": "3m", "2h": "3m", "3m": "4", "4": "6" };

const TAB_ROOMY = 170; // px per tab before folder names in the tabs give way

export class TabManager {
  constructor(tabbarEl, viewsEl, toolsEl, settings, onChange, onHelp, onSaveSession) {
    this.onSaveSession = onSaveSession;
    this.tabbarEl = tabbarEl;
    this.viewsEl = viewsEl;
    this.toolsEl = toolsEl;
    this.settings = settings;
    this.onChange = onChange;
    this.onHelp = onHelp;
    this.tabs = [];
    this.active = null;
    this.groups = new Map(); // open window layouts: id -> {name, template, slots[tabId], zoomed}
    this.bindTabbar();
    events.on("term:state", (st) => this.byId(st.id)?.onState(st));
    events.on("paste:progress", (p) => this.byId(p.id)?.onPasteProgress(p));
    events.on("term:log", ({ id, path }) => {
      const t = this.byId(id);
      if (t) { t.logPath = path; t.tabEl.title = `${t.subtitle()}\nLog: ${path}`; this.updateStatus(); this.renderTools(); }
    });
    events.on("sftp:progress", (p) => this.byId(p.tabId)?.sftp?.onProgress(p));
    // Registers Wails' window drop listeners; paths arrive for drops on --wails-drop-target elements.
    window.runtime.OnFileDrop((x, y, paths) => this.onFilesDropped(x, y, paths), true);
    const clearDrop = () => document.querySelectorAll(".sftp.dragover").forEach((el) => el.classList.remove("dragover"));
    ["drop", "dragend"].forEach((ev) => addEventListener(ev, clearDrop, true));
    addEventListener("dragleave", (e) => { if (!e.relatedTarget) clearDrop(); }, true);
    this.bindReorder();
  }

  /**
   * Many tabs: the mouse wheel scrolls the bar sideways, folder names in the
   * tabs give way to the session names, and "▾ n" lists every tab.
   */
  bindTabbar() {
    const bar = this.tabbarEl;
    bar.addEventListener("wheel", (e) => {
      if (bar.scrollWidth <= bar.clientWidth) return;
      e.preventDefault();
      bar.scrollLeft += Math.abs(e.deltaY) > Math.abs(e.deltaX) ? e.deltaY : e.deltaX;
    }, { passive: false });
    this.listBtn = h("button", { class: "tablist-btn", title: "Alle offenen Tabs", onclick: () => this.showTabList() });
    bar.after(this.listBtn);
    new ResizeObserver(() => this.updateTabbar()).observe(bar);
  }

  /** Crowded bar: hide folder names; overflowing bar: show the tab list button. */
  updateTabbar() {
    const bar = this.tabbarEl;
    const entries = syncBlocks(this);
    bar.classList.toggle("crowded", entries.length * TAB_ROOMY > bar.clientWidth);
    const overflow = bar.scrollWidth > bar.clientWidth + 1;
    this.listBtn.hidden = !overflow;
    this.listBtn.textContent = `▾ ${this.tabs.length}`;
  }

  showTabList() {
    const r = this.listBtn.getBoundingClientRect();
    contextMenu(r.left, r.bottom + 2, this.tabs.map((t) => {
      const folder = t.session ? this.folderPath(t.session.folderId).join(" › ") || "oberste Ebene" : "Quick Connect";
      const dot = { connected: "● ", connecting: "◌ ", queued: "◌ " }[t.state] || "○ ";
      return { label: (t.id === this.active ? "▸ " : "") + dot + t.title, hint: folder, action: () => this.activate(t.id) };
    }));
  }

  byId(id) { return this.tabs.find((t) => t.id === id); }
  current() { return this.byId(this.active); }

  open(opts, { activate = true, join = false } = {}) {
    const anchor = this.current();
    const tab = opts.session?.protocol === "rdp" ? new RdpTab(this, opts) : new Tab(this, opts);
    tab.renderFolder();
    this.tabs.push(tab);
    this.tabbarEl.append(tab.tabEl);
    // join: one more tile in the current view (the view grows). A session opened
    // normally stays out of it, so layouts keep their sessions.
    const view = this.groupOf(anchor);
    if (join && !opts.group && view && !view.zoomed) this.addToView(tab, anchor);
    if (activate) this.activate(tab.id);
    tab.connect();
    return tab;
  }

  /* ---------- window layouts (tiles) ---------- */

  /**
   * Opens a folder layout: every slot's session in its tile. An already open
   * layout of the same folder is just brought to front.
   */
  openLayout(folder, sessionsById) {
    const gid = "g" + folder.id;
    const existing = this.groups.get(gid);
    if (existing) {
      if (blockTab(this, existing)) { this.showGroup(existing); return; }
      this.dropGroup(gid);
    }
    const layout = folder.layout;
    const group = { id: gid, name: folder.name, template: layout.template, base: layout.template, slots: [], empties: [], zoomed: false };
    this.groups.set(gid, group);
    layout.slots.forEach((sid, slot) => {
      const session = sid && sessionsById.get(sid);
      group.slots[slot] = session ? this.open({ session, group: gid, slot }, { activate: false }).id : null;
    });
    const first = group.slots.find(Boolean);
    if (first) this.activate(first);
    else { this.dropGroup(gid); toast("Im Layout ist keine (vorhandene) Session eingetragen."); }
  }

  groupOf(tab) { return tab?.groupId ? this.groups.get(tab.groupId) : null; }

  /** Folder names from the root down to folderId (["Rechenzentrum", "Nord"]). */
  folderPath(folderId) {
    const out = [];
    for (let f = this.folders?.get(folderId), guard = 0; f && guard < 32; f = this.folders.get(f.parentId), guard++) out.unshift(f.name);
    return out;
  }

  /** Called after the tree loaded; relabels open tabs (folders renamed/moved). */
  setFolders(folders, sessions = []) {
    this.folders = new Map(folders.map((f) => [f.id, f]));
    const byId = new Map(sessions.map((s) => [s.id, s]));
    for (const t of this.tabs) {
      if (t.session && byId.has(t.session.id)) t.session = { ...t.session, folderId: byId.get(t.session.id).folderId };
      t.renderFolder();
    }
  }

  freeSlot(g) {
    const n = TEMPLATES[g.template]?.n || 1;
    for (let i = 0; i < n; i++) if (!g.slots[i]) return i;
    return -1;
  }

  /** Puts tab into view g: the wanted or a free tile, else the view grows (2 → 3 → 4 → 6). */
  joinGroup(tab, g, slot = -1) {
    if (slot < 0 || g.slots[slot]) slot = this.freeSlot(g);
    if (slot < 0) {
      const next = GROW[g.template];
      if (!next) { toast("Die Ansicht ist voll (6 Fenster) – als eigener Tab geöffnet."); return false; }
      g.template = next;
      slot = this.freeSlot(g);
    }
    g.slots[slot] = tab.id;
    g.zoomed = false;
    tab.setGroup(g.id, slot);
    return true;
  }

  /**
   * Adds tab to the view anchor is shown in. A single tab becomes a split
   * view of two; an open view gets one more tile.
   */
  addToView(tab, anchor = this.current()) {
    if (!anchor || anchor === tab) return false;
    let g = this.groupOf(anchor);
    if (g && tab.groupId === g.id) return true;
    if (!g) {
      g = { id: "v" + Date.now().toString(36), name: "Ansicht", template: "1", slots: [], empties: [], zoomed: false, adhoc: true };
      this.groups.set(g.id, g);
      this.joinGroup(anchor, g, 0);
    }
    if (this.freeSlot(g) < 0 && !GROW[g.template]) {
      toast("Die Ansicht ist voll (6 Fenster).");
      if (g.adhoc) this.compact(g);
      return false;
    }
    this.leaveGroup(tab);
    if (!this.joinGroup(tab, g)) {
      if (g.adhoc) this.compact(g);
      return false;
    }
    return true;
  }

  /** Moves an open tab into a tile of view gid (drag & drop). */
  moveToSlot(tab, gid, slot) {
    const g = this.groups.get(gid);
    if (!g || g.slots[slot]) return;
    if (tab.groupId === gid) { // within the layout: no leaving, or a view of two would dissolve
      g.slots[tab.slot] = null;
      g.slots[slot] = tab.id;
      tab.setGroup(gid, slot);
    } else {
      this.leaveGroup(tab);
      this.joinGroup(tab, g, slot);
    }
    this.activate(tab.id);
  }

  /** The tile showing element el (null outside of tiles). */
  tileAt(el) {
    return this.tabs.find((t) => t.groupId && t.view.contains(el)) || null;
  }

  /** Two tiles of a layout change places (drag & drop). */
  swapTiles(a, b) {
    const g = this.groupOf(a);
    if (!g || a === b || b.groupId !== g.id) return;
    const { slot } = a;
    g.slots[a.slot] = b.id;
    g.slots[b.slot] = a.id;
    a.setGroup(g.id, b.slot);
    b.setGroup(g.id, slot);
    this.activate(a.id);
  }

  /** Takes a tab out of its view; a self-made view shrinks again. */
  leaveGroup(tab) {
    const g = this.groupOf(tab);
    if (!g) return;
    g.slots[tab.slot] = null;
    tab.setGroup(null);
    this.afterRemove(g);
  }

  /**
   * A tile was emptied. One tab left → a plain tab again. Self-made views
   * close the gap; folder layouts shrink back to their saved size: tiles
   * that joined later move into gaps, only saved tiles may stay "frei".
   */
  afterRemove(g) {
    const ids = g.slots.filter((id) => id && this.byId(id));
    if (ids.length <= 1) {
      ids.forEach((id) => this.byId(id).setGroup(null));
      this.dropGroup(g.id);
      return;
    }
    if (g.adhoc) { this.compact(g); return; }
    const base = g.base || g.template;
    const baseN = TEMPLATES[base]?.n || 1;
    const extras = g.slots.slice(baseN).filter(Boolean);
    const slots = g.slots.slice(0, baseN);
    for (let i = 0; i < baseN && extras.length; i++) if (!slots[i]) slots[i] = extras.shift();
    slots.push(...extras);
    let t = base;
    while ((TEMPLATES[t]?.n || 1) < slots.length && GROW[t]) t = GROW[t];
    g.slots = slots;
    g.template = t;
    slots.forEach((id, i) => { if (id) this.byId(id)?.setGroup(g.id, i); });
  }

  detach(id) {
    const tab = this.byId(id);
    if (!tab) return;
    this.leaveGroup(tab);
    this.activate(tab.id);
  }

  /** Self-made views close gaps; one tab left = no view any more. */
  compact(g) {
    const ids = g.slots.filter((id) => id && this.byId(id));
    if (ids.length <= 1) {
      ids.forEach((id) => this.byId(id).setGroup(null));
      this.dropGroup(g.id);
      return;
    }
    g.slots = ids;
    g.template = templateFor(ids.length);
    ids.forEach((id, i) => this.byId(id).setGroup(g.id, i));
  }

  /** The tab whose view takes a new tile: the active layout, else the last opened one, else the active tab. */
  layoutAnchor() {
    const cur = this.current();
    if (this.groupOf(cur)) return cur;
    const last = [...this.groups.values()].pop();
    return this.byId(last?.slots.find(Boolean)) || cur || null;
  }

  /** Opens a session in a free tile of an open layout (drag & drop target). */
  fillSlot(gid, slot, session) {
    const g = this.groups.get(gid);
    if (!g || g.slots[slot]) return null;
    const tab = this.open({ session, group: gid, slot }, { activate: false });
    g.slots[slot] = tab.id;
    this.activate(tab.id);
    return tab;
  }

  dropGroup(gid) {
    const g = this.groups.get(gid);
    g?.empties.forEach((el) => el.remove());
    this.groups.delete(gid);
  }

  /** Lays out the views: the active tab alone, or its whole group as tiles. */
  arrange() {
    const tab = this.current();
    const group = this.groupOf(tab);
    const v = this.viewsEl;
    v.classList.toggle("grid", !!group);
    v.classList.toggle("zoomed", !!group?.zoomed);
    Object.assign(v.style, group && !group.zoomed ? gridStyle(group.template) : { gridTemplateAreas: "", gridTemplateColumns: "", gridTemplateRows: "" });
    for (const t of this.tabs) {
      const visible = group ? t.groupId === group.id && (!group.zoomed || t === tab) : t === tab;
      t.view.classList.toggle("active", visible);
      t.view.classList.toggle("focused", !!group && t === tab);
      t.tabEl.classList.toggle("active", t === tab);
    }
    for (const g of this.groups.values()) {
      g.empties.forEach((el) => el.remove());
      g.empties = [];
      if (g !== group || g.zoomed) continue;
      g.slots.forEach((tid, slot) => {
        if (tid) return;
        const el = h("div", {
          class: "slot-empty", style: { gridArea: "s" + slot }, "data-group": g.id, "data-slot": slot,
        }, "frei – Session hierher ziehen");
        v.append(el);
        g.empties.push(el);
      });
    }
    for (const t of this.tabs) if (t instanceof RdpTab && !t.view.classList.contains("active")) t.place();
    const visibleTabs = group ? this.tabs.filter((t) => t.view.classList.contains("active")) : [tab].filter(Boolean);
    requestAnimationFrame(() => visibleTabs.forEach((t) => t.refit()));
  }

  /** Alt+Arrow: focus the neighbouring tile in that direction. */
  focusDirection(dir) {
    const tab = this.current();
    const group = this.groupOf(tab);
    if (!group) return false;
    if (group.zoomed) { // only one tile shows: walk through them in order
      const list = this.tabs.filter((t) => t.groupId === group.id);
      const step = dir === "left" || dir === "up" ? -1 : 1;
      this.activate(list[(list.indexOf(tab) + step + list.length) % list.length].id);
      return true;
    }
    const r = tab.view.getBoundingClientRect();
    const cx = r.left + r.width / 2, cy = r.top + r.height / 2;
    let best = null, bestDist = Infinity;
    for (const t of this.tabs) {
      if (t === tab || t.groupId !== group.id) continue;
      const o = t.view.getBoundingClientRect();
      const ox = o.left + o.width / 2, oy = o.top + o.height / 2;
      const ok = { left: ox < cx - 1, right: ox > cx + 1, up: oy < cy - 1, down: oy > cy + 1 }[dir];
      if (!ok) continue;
      const d = Math.hypot(ox - cx, oy - cy);
      if (d < bestDist) { best = t; bestDist = d; }
    }
    if (best) this.activate(best.id);
    return true;
  }

  /** Alt+Enter: show the focused tile alone and back (like Hyprland fullscreen). */
  toggleZoom() {
    const group = this.groupOf(this.current());
    if (!group) return false;
    group.zoomed = !group.zoomed;
    this.arrange();
    this.current()?.focus();
    return true;
  }

  activate(id) {
    const tab = this.byId(id);
    if (!tab) return;
    this.active = id;
    const group = this.groupOf(tab);
    if (group) group.last = id;
    this.arrange();
    // Never pull the keyboard out of an open dialog (a password being typed …).
    // A click into the tile (SFTP list, search box) keeps its own focus as well.
    requestAnimationFrame(() => {
      const el = document.activeElement;
      const inside = tab.view.contains(el) && !el.classList.contains("xterm-helper-textarea");
      if (!overlayOpen() && !inside) tab.focus();
    });
    this.changed();
    (group?.blockEl || tab.tabEl).scrollIntoView({ inline: "nearest" });
  }

  close(id) {
    const i = this.tabs.findIndex((t) => t.id === id);
    if (i < 0) return;
    const [tab] = this.tabs.splice(i, 1);
    this.reordering = false; // a tab closed in mid-drag never gets its dragend
    const group = this.groupOf(tab);
    const mates = group ? group.slots.filter((x) => x && x !== tab.id) : [];
    if (group) {
      group.slots[tab.slot] = null;
      this.afterRemove(group);
    }
    tab.dispose();
    if (this.active === id) {
      const sibling = this.byId(group?.last) || this.byId(mates[0]); // stay in the view (or on its last tab)
      const near = sibling || this.tabs[i] || this.tabs[i - 1];
      const nearGroup = this.groupOf(near);
      const next = nearGroup ? blockTab(this, nearGroup) : near;
      this.active = null;
      if (next) this.activate(next.id);
      else this.arrange();
    } else this.arrange();
    this.changed();
  }

  /** Next or previous entry of the bar: a tab or a whole layout. */
  cycle(delta) {
    const entries = barEntries(this);
    if (!entries.length) return;
    const cur = this.current();
    const group = this.groupOf(cur);
    const i = entries.findIndex((e) => (group ? e.group === group : e.tab === cur));
    this.showEntry(entries[(i + delta + entries.length) % entries.length]);
  }

  /** Alt+1…9: the n-th entry of the bar. */
  showNth(n) {
    const e = barEntries(this)[n];
    if (e) this.showEntry(e);
  }

  showEntry(e) {
    if (e.tab) this.activate(e.tab.id);
    else this.showGroup(e.group);
  }

  showGroup(g) {
    const t = blockTab(this, g);
    if (t) this.activate(t.id);
  }

  /** Turns a layout back into single tabs. */
  dissolve(gid) {
    const g = this.groups.get(gid);
    if (!g) return;
    this.tabs.filter((t) => t.groupId === gid).forEach((t) => t.setGroup(null));
    this.dropGroup(gid);
    this.arrange();
    this.changed();
  }

  duplicate(id) {
    const t = this.byId(id || this.active);
    if (!t) return;
    this.activate(t.id); // the copy lands next to its source, not next to the active tab
    this.open({ session: t.session, target: t.target }, { join: true });
  }

  applySettings(s) {
    this.settings = s;
    this.tabs.forEach((t) => t.applySettings(s));
  }

  liveSessionIds() {
    return this.tabs.filter((t) => t.session && t.state === "connected").map((t) => t.session.id);
  }

  /** Opens a session; with sftp=true the SFTP browser opens too (reusing a live tab). */
  openSession(session, { sftp = false, join = false } = {}) {
    if (sftp) {
      const live = this.tabs.find((t) => t.session?.id === session.id && t.state === "connected");
      if (live) { this.activate(live.id); live.openSftp(); return live; }
    }
    return this.open({ session, sftp }, { join });
  }

  changed() {
    this.updateTabbar();
    this.updateStatus();
    this.renderTools();
    this.onChange?.(this);
  }

  /** Visible buttons for the active tab; the tooltips name the shortcuts. */
  renderTools() {
    const t = this.current();
    if (!t) { this.toolsEl.replaceChildren(); return; }
    const ssh = t.protocol === "ssh";
    if (t.protocol === "rdp") { // a remote desktop has no terminal tools
      this.toolsEl.replaceChildren(h("button", { class: "btn ghost", title: "Alle Tastenkürzel (Alt+K)", onclick: () => this.onHelp?.() }, h("span", {}, "⌨"), "Kürzel"));
      return;
    }
    const btn = (icon, label, title, onclick, opts = {}) => h("button", {
      class: "btn ghost" + (opts.on ? " on" : ""), title, onclick, disabled: opts.disabled,
    }, h("span", {}, icon), label);
    this.toolsEl.replaceChildren(
      t.session ? "" : btn("💾", "Speichern", "Diese Quick-Connect-Verbindung als Session speichern", () => this.onSaveSession?.(t)),
      btn("📁", "SFTP", ssh ? "SFTP-Browser ein/aus (Alt+B)" : "SFTP gibt es nur bei SSH-Verbindungen",
        () => t.toggleSftp(), { on: t.sftpOpen, disabled: !ssh }),
      btn("📊", "Status", "Server-Status der Gegenstelle: Last, RAM, Platte, Prozesse (Alt+I)", () => this.statusPanel?.toggle(), { on: !!this.statusPanel?.open }),
      btn("⚡", "Schnipsel", "Befehls-Schnipsel in diesen Tab tippen (Alt+J)", (e) => this.onSnippets?.(e.currentTarget)),
      btn("🔍", "Suchen", "Im Terminal suchen (Alt+G)", () => t.openSearch()),
      btn("📄", "Log", t.logPath ? `Log dieser Sitzung zeigen\n${t.logPath}` : "Diese Sitzung wird nicht protokolliert",
        () => t.revealLog(), { disabled: !t.logPath }),
      btn("⌨", "Kürzel", "Alle Tastenkürzel (Alt+K)", () => this.onHelp?.()),
    );
  }

  updateStatus() {
    const left = document.getElementById("status-left");
    const right = document.getElementById("status-right");
    const t = this.current();
    if (!t) { left.textContent = "Bereit"; right.textContent = `${this.tabs.length} Tabs`; return; }
    const states = { connected: "verbunden", connecting: "verbinde…", queued: "wartet auf vorige Anmeldung", closed: "getrennt", error: "Fehler", new: "" };
    const logging = t.logPath && t.state === "connected";
    left.replaceChildren(
      logging ? h("span", { class: "rec", title: `Wird protokolliert: ${t.logPath}\n(Klick zeigt die Datei)`, onclick: () => t.revealLog() }, "● LOG") : "",
      `${t.subtitle()} · ${states[t.state] || t.state}`);
    const size = t.term ? `${t.term.cols}×${t.term.rows} · ` : "";
    right.textContent = `${size}${this.tabs.length} Tab${this.tabs.length === 1 ? "" : "s"}`;
  }

  onFilesDropped(x, y, paths) {
    const el = document.elementFromPoint(x, y);
    const view = el?.closest(".tabview");
    const tab = this.tabs.find((t) => t.view === view);
    if (!paths?.length) { toast("Drag & Drop hat keine Dateipfade geliefert – nimm „⬆ Hochladen“ oder den lokalen Dateibrowser.", "err"); return; }
    if (tab?.sftp && el.closest(".sftp")) tab.sftp.upload(paths);
  }

  bindReorder() {
    let dragging = null;
    this.tabbarEl.addEventListener("dragstart", (e) => {
      dragging = e.target.closest(".tab");
      if (!dragging) return;
      this.reordering = true; // the bar must not be rebuilt under the drag
      const tab = this.tabs.find((t) => t.tabEl === dragging);
      // A layout block only moves along the bar; a single tab can also go into a view.
      e.dataTransfer.setData(tab ? TAB_DRAG : "text/plain", tab ? tab.id : "");
      dragging.classList.add("dragging");
    });
    this.tabbarEl.addEventListener("dragover", (e) => {
      if (!dragging) return;
      e.preventDefault();
      const over = e.target.closest(".tab");
      if (!over || over === dragging) return;
      const r = over.getBoundingClientRect();
      over[e.clientX < r.left + r.width / 2 ? "before" : "after"](dragging);
    });
    this.tabbarEl.addEventListener("dragend", () => {
      if (!dragging) return;
      dragging.classList.remove("dragging");
      this.reordering = false;
      const order = [...this.tabbarEl.querySelectorAll(".tab:not(.tab-block)")];
      this.tabs.sort((a, b) => order.indexOf(a.tabEl) - order.indexOf(b.tabEl));
      dragging = null;
    });
  }
}
