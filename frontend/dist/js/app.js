// App bootstrap: wires sidebar, tabs, dialogs and keyboard shortcuts.
import { api, errMsg, events } from "./api.js";
import { cancelPrompt, editSession, editSettings, firstRunSetup, handlePrompt } from "./dialogs.js";
import { editLayout } from "./layoutEditor.js";
import { editManySessions } from "./bulkEdit.js";
import { initFirmware, openFirmware } from "./firmware.js";
import { editTask, initTasks, openTasks } from "./tasks.js";
import { initMcp } from "./mcp.js";
import { exportShare, importShare } from "./share.js";
import { StatusPanel } from "./statusPanel.js";
import { showShortcuts } from "./shortcuts.js";
import { DRAG_TYPE, SessionTree } from "./tree.js";
import { checkNow, initUpdates, showVersion } from "./update.js";
import { manageSnippets, snippetMenu } from "./snippets.js";
import { TAB_DRAG, TabManager } from "./tabs.js";
import { confirmDialog, contextMenu, h, icon, inputDialog, makeSplitter, modal, setDragActive, store, toast } from "./ui.js";
import { GLOBAL_IN_FIELDS, keyAction } from "./keys.js";

const $ = (id) => document.getElementById(id);

let settings = null;
let tree = null;
let tabs = null;
let treeData = { folders: [], sessions: [] };
let statusPanel = null;

async function reloadTree() {
  try {
    treeData = await api.GetTree();
    tree.set(treeData);
    tabs?.setFolders(treeData.folders, treeData.sessions);
    renderRecent();
  } catch (e) {
    toast("Sessions laden: " + errMsg(e), "err");
  }
}

function connect(session, sftp = false, { join = false } = {}) {
  const tab = tabs.openSession(session, { sftp, join });
  setTimeout(reloadTree, 300); // lastUsed changed
  return tab;
}

/** Opens sessions as tiles next to anchor and leaves the keyboard in the tree for the next one. */
function openBeside(anchor, sessions) {
  let first = anchor;
  for (const s of sessions) {
    const tab = connect(s, false, { join: false });
    if (first && tab && tabs.addToView(tab, first)) tabs.activate(tab.id);
    first ||= tab;
  }
  requestAnimationFrame(() => tree.el.focus());
}

const joinLayout = (sessions) => openBeside(tabs.layoutAnchor(), sessions);
const newBlock = (sessions) => openBeside(null, sessions);

/** RDP sessions open in Windows' Remote Desktop, not in a tab. */
async function connectRDP(s) {
  try {
    await api.ConnectRDP(s.id);
    toast(`Remotedesktop zu ${s.host} wird geöffnet …`, "ok");
    setTimeout(reloadTree, 300);
  } catch (e) { toast("Remotedesktop: " + errMsg(e), "err"); }
}

function quickConnect(target) {
  const t = target.trim();
  if (t) tabs.open({ target: t });
}

async function newSession(folderId = "", preset = {}) {
  const res = await editSession({ folderId, ...preset }, treeData.folders, treeData.sessions);
  if (!res) return;
  await reloadTree();
  tree.select("session", res.session.id);
  if (res.connect) connect(res.session);
}

/** Quick-connect tab → saved session; the open tab keeps running and adopts it. */
async function saveTabAsSession(tab) {
  const t = tab.quickTarget || {};
  const host = t.host || tab.target;
  const res = await editSession({ name: host, host, user: t.user || "", port: t.port || 22 }, treeData.folders, treeData.sessions);
  if (!res) return;
  tab.adoptSession(res.session);
  await reloadTree();
  tree.select("session", res.session.id);
  toast(`Session „${res.session.name}“ gespeichert.`, "ok");
  if (res.connect && tab.state !== "connected") tab.connect();
}

async function editExisting(s) {
  const res = await editSession(s, treeData.folders, treeData.sessions);
  if (!res) return;
  await reloadTree();
  if (res.connect) connect(res.session);
}

async function newFolder(parentId = "") {
  const name = await inputDialog({ title: "Neuer Ordner", label: "Name", ok: "Anlegen" });
  if (!name) return;
  try {
    const f = await api.SaveFolder({ id: "", name, parentId });
    if (parentId) tree.toggle(parentId, true);
    await reloadTree();
    tree.select("folder", f.id);
  } catch (e) { toast(errMsg(e), "err"); }
}

async function renameFolder(f) {
  const name = await inputDialog({ title: "Ordner umbenennen", label: "Name", value: f.name, ok: "Umbenennen" });
  if (!name || name === f.name) return;
  try { await api.SaveFolder({ ...f, name }); await reloadTree(); } catch (e) { toast(errMsg(e), "err"); }
}

/** All sessions inside a folder and its subfolders. */
function sessionsInFolder(folderId) {
  const ids = new Set([folderId]);
  for (let grew = true; grew;) {
    grew = false;
    for (const f of treeData.folders) if (ids.has(f.parentId) && !ids.has(f.id)) { ids.add(f.id); grew = true; }
  }
  return treeData.sessions.filter((s) => ids.has(s.folderId));
}

async function removeItem(type, item) {
  if (type === "folder") return removeFolder(item);
  const ok = await confirmDialog({
    title: "Session löschen",
    message: `Session „${item.name}“ löschen? Ein gespeichertes Passwort wird mit gelöscht.`,
    ok: "Löschen",
    danger: true,
  });
  if (!ok) return;
  act(api.DeleteSession, item.id);
}

/** Folder with sessions: ask whether they go too or move one level up. */
async function removeFolder(f) {
  const inside = sessionsInFolder(f.id);
  if (!inside.length) {
    if (await confirmDialog({ title: "Ordner löschen", message: `Leeren Ordner „${f.name}“ löschen?`, ok: "Löschen", danger: true })) act(api.DeleteFolder, f.id, false);
    return;
  }
  const n = inside.length;
  const choice = await modal((close) => ({
    head: "Ordner löschen",
    danger: true,
    body: h("div", {},
      h("p", {}, `„${f.name}“ enthält ${n} Session${n === 1 ? "" : "s"} (mit Unterordnern):`),
      h("div", { class: "del-list" }, inside.slice(0, 12).map((s) => h("div", {}, s.name)), n > 12 ? h("div", { class: "muted" }, `… und ${n - 12} weitere`) : null),
      h("p", { class: "hint" }, "„Nur Ordner“: die Sessions wandern eine Ebene nach oben. Beim Mitlöschen gehen auch ihre gespeicherten Passwörter verloren.")),
    foot: [
      h("button", { class: "btn left", onclick: () => close(null) }, "Abbrechen"),
      h("button", { class: "btn", onclick: () => close("keep") }, "Nur Ordner"),
      h("button", { class: "btn primary danger", onclick: () => close("all") }, `Mit ${n} Session${n === 1 ? "" : "s"} löschen`),
    ],
  }));
  if (choice) act(api.DeleteFolder, f.id, choice === "all");
}

/** Several marked sessions (Ctrl/Shift+click). */
async function removeSessions(list) {
  const n = list.length;
  const ok = await confirmDialog({
    title: `${n} Sessions löschen`,
    message: `${list.slice(0, 10).map((s) => s.name).join(", ")}${n > 10 ? ` … und ${n - 10} weitere` : ""} löschen? Gespeicherte Passwörter werden mit gelöscht.`,
    ok: `${n} löschen`,
    danger: true,
  });
  if (ok) act(api.DeleteSessions, list.map((s) => s.id));
}

function sessionsMenu(list, x, y) {
  const n = list.length;
  contextMenu(x, y, [
    { label: `${n} Sessions verbinden`, action: () => list.forEach((s) => connect(s)) },
    { label: `${n} Sessions in eine geteilte Ansicht`, disabled: n > 6, action: () => {
      const first = connect(list[0], false, { join: false });
      list.slice(1).forEach((s) => { const t = connect(s, false, { join: false }); if (first && t) tabs.addToView(t, first); });
      if (first) tabs.activate(first.id);
    } },
    { label: `In Oberste Ebene verschieben`, action: async () => { for (const s of list) await api.MoveSession(s.id, "").catch(() => {}); reloadTree(); } },
    "-",
    { label: `${n} Sessions löschen`, hint: "Entf", danger: true, action: () => removeSessions(list) },
  ]);
}

async function act(fn, ...args) {
  try { await fn(...args); await reloadTree(); } catch (e) { toast(errMsg(e), "err"); }
}

function sessionMenu(s, x, y) {
  contextMenu(x, y, [
    { label: "Verbinden", hint: "Enter", action: () => connect(s) },
    s.protocol === "rdp"
      ? { label: "In eigenem Remotedesktop-Fenster öffnen", action: () => connectRDP(s) }
      : { label: "📁 Verbinden mit SFTP-Browser", disabled: s.protocol !== "ssh", action: () => connect(s, true) },
    { label: "⚙ Befehle ausführen / planen…", disabled: s.protocol === "rdp", action: () => editTask({ name: `Befehle auf ${s.name}`, sessionIds: [s.id] }) },
    "-",
    { label: "Bearbeiten…", hint: "F2", action: () => editExisting(s) },
    { label: "Duplizieren", action: () => act(api.DuplicateSession, s.id) },
    { label: "In Oberste Ebene verschieben", disabled: !s.folderId, action: () => act(api.MoveSession, s.id, "") },
    { label: "📤 Exportieren zum Teilen…", action: () => exportShare({ sessionIds: [s.id], label: `Session „${s.name}“` }) },
    "-",
    { label: "Löschen", hint: "Entf", danger: true, action: () => removeItem("session", s) },
  ]);
}

/** After an import: sessions and maybe settings changed. */
async function afterImport() {
  await reloadTree();
  settings = await api.GetSettings();
  applyTheme();
  tabs.applySettings(settings);
}

async function runImport() {
  if (await importShare()) await afterImport();
}

function openLayout(folder) {
  tabs.openLayout(folder, new Map(treeData.sessions.map((s) => [s.id, s])));
  setTimeout(reloadTree, 300);
}

async function setupLayout(folder) {
  const res = await editLayout(folder, treeData.sessions);
  if (!res) return;
  await reloadTree();
  if (res.folder.layout) {
    toast(`Layout für „${folder.name}“ gespeichert – ▶ am Ordner öffnet es.`, "ok");
    if (res.open) openLayout(res.folder);
  } else toast("Layout entfernt.");
}

function folderMenu(f, x, y) {
  contextMenu(x, y, [
    { label: "▶ Fenster-Layout öffnen", disabled: !f.layout, action: () => openLayout(f) },
    { label: f.layout ? "▦ Fenster-Layout bearbeiten…" : "▦ Fenster-Layout einrichten…", action: () => setupLayout(f) },
    "-",
    { label: "Neue Session hier…", action: () => newSession(f.id) },
    { label: "Neuer Unterordner…", action: () => newFolder(f.id) },
    { label: "Alle verbinden", action: () => treeData.sessions.filter((s) => s.folderId === f.id).forEach((s) => connect(s)) },
    { label: "Für alle Sessions ändern… (Benutzer, Passwort …)", action: () => bulkEdit(f) },
    { label: "Umbenennen…", hint: "F2", action: () => renameFolder(f) },
    { label: "In Oberste Ebene verschieben", disabled: !f.parentId, action: () => act(api.MoveFolder, f.id, "") },
    { label: "📤 Ordner exportieren zum Teilen…", action: () => exportShare({ folderIds: [f.id], label: `Ordner „${f.name}“ mit Unterordnern und Layout` }) },
    "-",
    { label: "Ordner löschen", hint: "Entf", danger: true, action: () => removeItem("folder", f) },
  ]);
}

/** Folder (or null = all sessions): one change for all its sessions. */
async function bulkEdit(folder) {
  if (await editManySessions(folder, treeData)) await reloadTree();
}

function rootMenu(x, y) {
  contextMenu(x, y, [
    { label: "Neue Session…", action: () => newSession("") },
    { label: "Neuer Ordner…", action: () => newFolder("") },
    { label: "Für alle Sessions ändern… (Benutzer, Passwort …)", action: () => bulkEdit(null) },
    "-",
    { label: "📤 Alles exportieren zum Teilen…", action: () => exportShare({ all: true, label: "alle Sessions und Ordner" }) },
    { label: "📥 easyTTY-Export importieren…", action: runImport },
    { label: "PuTTY-Sessions importieren", action: importPutty },
    { label: "SecureCRT-Sessions importieren", action: () => importSecureCRT() },
  ]);
}

async function importPutty() {
  try {
    const r = await api.ImportPutty();
    if (!r.imported && !r.skipped) toast("Keine PuTTY-Sessions gefunden.");
    else toast(`${r.imported} Session(s) importiert${r.skipped ? `, ${r.skipped} übersprungen (Telnet/Raw/leer)` : ""}.`, "ok", 6000);
    await reloadTree();
  } catch (e) { toast("Import: " + errMsg(e), "err"); }
}

/** SecureCRT: finds its Sessions folder itself, otherwise asks for folder or XML export. */
async function importSecureCRT(from = "") {
  try {
    const r = await api.ImportSecureCRT(from);
    if (r.notFound) {
      const pick = await modal((close) => ({
        head: "SecureCRT importieren",
        body: h("div", {},
          h("p", {}, "Auf diesem PC ist kein SecureCRT-Sessions-Ordner zu finden."),
          h("p", { class: "hint" }, "Entweder den Ordner Config\\Sessions wählen (z. B. vom alten PC kopiert) oder in SecureCRT unter Tools → Export Settings eine XML-Datei mit den Sessions erzeugen.")),
        foot: [
          h("button", { class: "btn left", onclick: () => close(null) }, "Abbrechen"),
          h("button", { class: "btn", onclick: () => close("dir") }, "Sessions-Ordner wählen…"),
          h("button", { class: "btn primary", onclick: () => close("xml") }, "XML-Export wählen…"),
        ],
      }));
      if (pick) await importSecureCRT(pick);
      return;
    }
    if (!r.source) return; // dialog cancelled
    const skipped = r.skipped || [];
    const parts = [`${r.imported} Session(s) aus SecureCRT importiert`];
    if (r.existing) parts.push(`${r.existing} schon vorhanden`);
    if (skipped.length) parts.push(`${skipped.length} übersprungen (nur SSH, Seriell und RDP): ${skipped.slice(0, 5).join(", ")}${skipped.length > 5 ? " …" : ""}`);
    toast(parts.join(", ") + ". Passwörter bitte neu eingeben.", r.imported ? "ok" : "", 9000);
    await reloadTree();
  } catch (e) { toast("SecureCRT-Import: " + errMsg(e), "err"); }
}

async function openSettings() {
  const s = await editSettings(settings, importPutty, reloadTree, {
    exportAll: () => exportShare({ all: true, label: "alle Sessions, Ordner und Einstellungen" }),
    exportSettings: () => exportShare({ label: "nur Einstellungen", all: false, settingsOnly: true }),
    importShare: runImport,
    importSecureCRT: () => importSecureCRT(),
    checkUpdate: () => checkNow(() => tabs.tabs.length),
  });
  if (!s) return;
  settings = s;
  applyTheme();
  tabs.applySettings(s);
}

function applyTheme() {
  document.documentElement.dataset.theme = settings.theme;
}

function renderRecent() {
  const box = $("recent");
  const recent = tree.recent();
  box.replaceChildren(...recent.map((s) => h("div", { class: "recent-card", onclick: () => connect(s) },
    h("div", { class: "t" }, s.color ? h("span", { style: { color: s.color } }, "● ") : null, s.name),
    h("div", { class: "h" }, s.protocol === "serial" ? s.serialPort : (s.user ? s.user + "@" : "") + s.host))));
}

let shownTab = ""; // "tabId|sessionId" the tree selection was last synced to

function onTabsChanged(mgr) {
  $("welcome").style.display = mgr.tabs.length ? "none" : "";
  tree.setLive(mgr.liveSessionIds());
  // The tree highlights the session of the active tab – nothing for a quick
  // connect, so it never looks as if a saved session were open. Only on a tab
  // switch, so a selection made in the tree in between is left alone.
  const cur = mgr.current();
  const key = cur ? `${cur.id}|${cur.session?.id || ""}` : "";
  if (key !== shownTab) {
    shownTab = key;
    if (cur?.session?.id) tree.select("session", cur.session.id);
    else tree.clearSelection();
  }
  statusPanel?.refresh();
}

/** RDP windows lie over the page: without this a drag could never land on one. */
function bindDragOverRdp() {
  addEventListener("dragstart", (e) => {
    const { types } = e.dataTransfer;
    if (types.includes(DRAG_TYPE) || types.includes(TAB_DRAG)) setDragActive(true);
  });
  addEventListener("drop", () => setDragActive(false), true);
  addEventListener("dragend", () => {
    setDragActive(false);
    const tab = tabs.current();
    if (tab?.protocol === "rdp") tab.focus(); // a cancelled drag: the keyboard goes back
  });
  // Should a drag end without an event, the next click brings the window back.
  addEventListener("mousedown", () => setDragActive(false), true);
}

/**
 * Drag a session from the tree onto the workspace (or tab bar) to open it;
 * onto a free layout tile to open it right there. A folder opens its layout,
 * or all its sessions when it has none.
 */
function bindWorkspaceDrop() {
  const views = $("tabviews");
  let marked = null; // the tile a dragged tab would land on
  const markTile = (tile) => {
    if (tile === marked) return;
    marked?.view.classList.remove("drop-tile");
    tile?.view.classList.add("drop-tile");
    marked = tile;
  };
  addEventListener("dragend", () => markTile(null));
  for (const el of [views, $("tabbar")]) {
    el.addEventListener("dragover", (e) => {
      const tabDrag = el === views && e.dataTransfer.types.includes(TAB_DRAG);
      if (!e.dataTransfer.types.includes(DRAG_TYPE) && !tabDrag) return;
      e.preventDefault();
      el.classList.add("drop-open");
      e.target.closest?.(".slot-empty")?.classList.add("drop");
      if (tabDrag) markTile(tabs.tileAt(e.target));
    });
    el.addEventListener("dragleave", (e) => {
      e.target.closest?.(".slot-empty")?.classList.remove("drop");
      if (!el.contains(e.relatedTarget)) { el.classList.remove("drop-open"); markTile(null); }
    });
    el.addEventListener("drop", (e) => {
      el.classList.remove("drop-open");
      markTile(null);
      const slot = e.target.closest?.(".slot-empty");
      const tabId = el === views && e.dataTransfer.getData(TAB_DRAG);
      if (tabId) { // a tab from the tab bar: into this view
        e.preventDefault();
        const tab = tabs.byId(tabId);
        if (!tab) return;
        const over = tabs.tileAt(e.target);
        if (slot) tabs.moveToSlot(tab, slot.dataset.group, +slot.dataset.slot);
        else if (over?.groupId && over.groupId === tab.groupId) tabs.swapTiles(tab, over);
        else if (tabs.addToView(tab)) tabs.activate(tab.id);
        return;
      }
      const raw = e.dataTransfer.getData(DRAG_TYPE);
      if (!raw) return;
      e.preventDefault();
      let item;
      try { item = JSON.parse(raw); } catch { return; }
      if (item.type === "folder") {
        const f = treeData.folders.find((x) => x.id === item.id);
        if (f?.layout) openLayout(f);
        else treeData.sessions.filter((s) => s.folderId === item.id).forEach((s) => connect(s));
        return;
      }
      const s = treeData.sessions.find((x) => x.id === item.id);
      if (!s) return;
      if (slot) { tabs.fillSlot(slot.dataset.group, +slot.dataset.slot, s); setTimeout(reloadTree, 300); return; }
      if (el !== views) { connect(s, false, { join: false }); return; } // tab bar: own tab
      // Workspace: a single tab becomes a split view, an open view grows.
      const anchor = tabs.current();
      const tab = connect(s, false, { join: false });
      if (anchor && tab && tabs.addToView(tab, anchor)) tabs.activate(tab.id);
    });
  }
}

const SIDEBAR_KEY = "easytty.sidebarHidden";

function setSidebar(hidden) {
  $("sidebar").classList.toggle("hidden", hidden);
  $("sidebar-splitter").classList.toggle("hidden", hidden);
  $("btn-sidebar").classList.toggle("on", !hidden);
  store.set(SIDEBAR_KEY, hidden);
  requestAnimationFrame(() => tabs?.current()?.refit());
}

/** Alt+E: into the session tree (showing the sidebar), again: back to the terminal. */
function focusTree() {
  if ($("sidebar").contains(document.activeElement) && !$("sidebar").classList.contains("hidden")) {
    tabs.current()?.focus();
    return;
  }
  if ($("sidebar").classList.contains("hidden")) setSidebar(false);
  tree.el.focus();
  if (!tree.selected) tree.moveSelection(1);
}

function toggleSidebar() { setSidebar(!$("sidebar").classList.contains("hidden")); }

/** Runs a keymap action (see keys.js). Returns false if it did not apply. */
/** Snippet menu below anchor (the tab bar button), or in the middle of the active tab. */
function openSnippets(anchor = document.querySelector("#tabtools button[title^='Befehls-Schnipsel']")) {
  const tab = tabs.current();
  const r = anchor?.getBoundingClientRect() || tab?.view?.getBoundingClientRect() || { left: 200, bottom: 120 };
  snippetMenu(r.left, r.bottom + 4, settings.snippets, {
    canSend: !!tab && tab.protocol !== "rdp" && tab.state === "connected",
    send: (sn) => (sn.saveOutput ? tabs.current()?.runSnippetToFile(sn) : tabs.current()?.sendSnippet(sn.text)),
    onCancel: () => tabs.current()?.focus(),
    manage: async () => {
      const saved = await manageSnippets(settings.snippets);
      if (!saved) return;
      settings = saved;
      tabs.applySettings(saved);
      toast("Schnipsel gespeichert.", "ok");
    },
  });
}

function runAction(action) {
  const tab = tabs.current();
  const [name, arg] = action.split(":");
  switch (name) {
    case "quick": $("quick-input").focus(); $("quick-input").select(); return true;
    case "sidebar": toggleSidebar(); return true;
    case "help": showShortcuts(); return true;
    case "status": statusPanel.toggle(); return true;
    case "tree": focusTree(); return true;
    case "terminal": tabs.current()?.focus(); return true;
    case "firmware": openFirmware(tabs); return true;
    case "tasks": openTasks(); return true;
    case "next": tabs.cycle(1); return true;
    case "prev": tabs.cycle(-1); return true;
    case "tab": tabs.showNth(+arg - 1); return true;
    case "dir":
      // In a window layout: neighbour tile; otherwise previous/next tab.
      if (tabs.groupOf(tab)) return tabs.focusDirection(arg);
      if (arg === "left") tabs.cycle(-1); else if (arg === "right") tabs.cycle(1);
      return true;
  }
  if (!tab) return false;
  switch (name) {
    case "close": tabs.close(tab.id); return true;
    case "zoom": if (!tabs.toggleZoom()) toast("Vollbild-Fenster gibt es nur in einem Fenster-Layout (▶ ▦ am Ordner)."); return true;
    case "duplicate": tabs.duplicate(tab.id); return true;
    case "sftp": tab.sftpShortcut(); return true;
    case "search": tab.openSearch(); return true;
    case "copy": tab.copy(); return true;
    case "paste": tab.paste(); return true;
    case "log": tab.revealLog(); return true;
    case "font": tab.zoom(arg === "+" ? 1 : arg === "-" ? -1 : 0); return true;
    case "snippets": openSnippets(); return true;
  }
  return false;
}

function onKeyDown(e) {
  const action = keyAction(e, { altKeys: settings.altShortcuts !== false });
  if (!action) return;
  // Never act on tabs behind a dialog; form fields keep most of their keys.
  if ($("layer").childElementCount) return;
  const inField = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName) && !document.activeElement.closest(".xterm");
  if (inField && !GLOBAL_IN_FIELDS.has(action.split(":")[0])) return;
  if (runAction(action)) e.preventDefault();
}

async function main() {
  settings = await api.GetSettings();
  applyTheme();
  if (!settings.firstRunDone) settings = await firstRunSetup(settings);

  tree = new SessionTree($("tree"), $("tree-filter"), {
    connect,
    joinLayout,
    newBlock,
    menuSession: sessionMenu,
    menuFolder: folderMenu,
    menuRoot: rootMenu,
    moveSession: (id, folderId) => act(api.MoveSession, id, folderId),
    moveFolder: (id, parentId) => act(api.MoveFolder, id, parentId),
    openLayout,
    edit: (type, item) => (type === "session" ? editExisting(item) : renameFolder(item)),
    remove: removeItem,
    removeSessions,
    menuSessions: sessionsMenu,
    leave: () => tabs.current()?.focus(),
  });
  tabs = new TabManager($("tabbar"), $("tabviews"), $("tabtools"), settings, onTabsChanged, showShortcuts, saveTabAsSession);
  $("btn-shortcuts").addEventListener("click", showShortcuts);
  statusPanel = new StatusPanel($("infopanel"), tabs);
  tabs.statusPanel = statusPanel;
  tabs.onSnippets = (anchor) => openSnippets(anchor);
  bindWorkspaceDrop();
  bindDragOverRdp();
  makeSplitter($("sidebar-splitter"), $("sidebar"), { min: 180, max: 520, storageKey: "easytty.sidebarWidth", onEnd: () => tabs.current()?.refit() });

  events.on("prompt", (ev) => handlePrompt(ev, tabs.byId(ev.tabId)?.title));
  events.on("prompt:cancel", cancelPrompt);
  events.on("notify", (n) => toast(n.message, n.kind || "info", 7000));
  events.on("sessions:changed", () => reloadTree());
  initUpdates($("status-update"), () => tabs.tabs.length);
  showVersion($("status-version"), $("welcome-version"), () => tabs.tabs.length);
  events.on("rdp:state", (e) => tabs.byId(e.id)?.onState?.(e));
  // An easyTTY shortcut typed while a remote desktop had the keyboard (Alt+W …):
  // it belongs to that session's tab, which may not be the active tile.
  events.on("rdp:key", (e) => {
    if (!tabs.byId(e.id)) return;
    tabs.activate(e.id);
    document.dispatchEvent(new KeyboardEvent("keydown", { key: e.message, altKey: true, bubbles: true, cancelable: true }));
  });

  $("quick").addEventListener("submit", (e) => {
    e.preventDefault();
    quickConnect($("quick-input").value);
    $("quick-input").value = "";
  });
  $("quick-input").addEventListener("keydown", (e) => {
    if (e.key === "Escape") { e.target.value = ""; tabs.current()?.focus(); }
    if (e.key === "Enter" && e.ctrlKey) {
      e.preventDefault();
      // Ctrl+Enter: save as session instead of connecting once.
      const v = e.target.value.trim();
      const m = v.match(/^(?:([^@]+)@)?([^:\s]+)(?::(\d+))?$/);
      e.target.value = "";
      newSession("", m ? { host: m[2], user: m[1] || "", port: m[3] ? +m[3] : 22, name: m[2] } : {});
    }
  });
  $("btn-new-session").addEventListener("click", () => newSession(tree.selected?.type === "folder" ? tree.selected.id : ""));
  $("btn-new-folder").addEventListener("click", () => newFolder(""));
  $("btn-settings").addEventListener("click", openSettings);
  $("btn-tasks").append(icon("tasks"));
  $("btn-tasks").addEventListener("click", openTasks);
  initTasks(() => tabs.current()?.session || null);
  initMcp();
  // AI runs type into visible tabs: open one with the backend's id, or bring it to front.
  events.on("tab:open", ({ tabId, sessionId }) => {
    const s = treeData?.sessions.find((x) => x.id === sessionId);
    if (s && !tabs.byId(tabId)) tabs.open({ session: s, id: tabId }, { join: false });
  });
  events.on("tab:focus", (tabId) => tabs.activate(tabId));
  events.on("tab:close", (tabId) => { const t = tabs.byId(tabId); if (t && t.state !== "connected") tabs.close(tabId); });
  events.on("tab:ai", ({ tabId, client, active }) => tabs.byId(tabId)?.setAI(active ? client : null));
  $("btn-firmware").append(icon("firmware"));
  $("btn-firmware").addEventListener("click", () => openFirmware(tabs));
  initFirmware((on) => $("btn-firmware").classList.toggle("running", on), () => tabs.current());
  $("btn-sidebar").append(icon("sidebar"));
  $("btn-sidebar").addEventListener("click", toggleSidebar);
  setSidebar(!!store.get(SIDEBAR_KEY, false));
  document.addEventListener("keydown", onKeyDown);
  // Keep the terminal from losing every Ctrl+Wheel to page zoom.
  addEventListener("wheel", (e) => { if (e.ctrlKey) { e.preventDefault(); tabs.current()?.zoom(e.deltaY < 0 ? 1 : -1); } }, { passive: false });

  await reloadTree();
  onTabsChanged(tabs);
  $("tree").focus();
}

main().catch((e) => {
  document.body.prepend(h("div", { style: { padding: "20px", color: "#ff5d6c" } }, "Startfehler: " + errMsg(e)));
});
