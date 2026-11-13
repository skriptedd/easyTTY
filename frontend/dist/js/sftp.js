// SFTP panel of an SSH tab: server pane (top), this PC (bottom), transfers.
import { api, clipboard, errMsg } from "./api.js";
import { confirmDialog, contextMenu, fmtDate, fmtSize, h, icon, inputDialog, modal, store, toast } from "./ui.js";

const DONE_LINGER_MS = 5000;
const DRAG_TYPE = "application/x-easytty-files";
const LOCAL_DIR_KEY = "easytty.localDir";

const remoteParent = (p) => {
  if (!p || p === "/") return "/";
  const i = p.replace(/\/+$/, "").lastIndexOf("/");
  return i <= 0 ? "/" : p.slice(0, i);
};
const join = (dir, name) => (dir.endsWith("/") ? dir : dir + "/") + name;
const baseName = (p) => p.split(/[\\/]/).filter(Boolean).pop() || p;
const shellQuote = (s) => "'" + s.replace(/'/g, "'\\''") + "'";

/** One file list (server or PC) with path bar, selection, keyboard and drag & drop. */
class FilePane {
  /**
   * opts: { kind: "remote"|"local", title, load(path), parent(path), home(), rootLabel,
   *         onOpenFile(entry), onMenu(x, y, entry|null), onDropFiles({kind, paths}, targetDir),
   *         onKey(e, sel), emptyText, tools: [buttons] }
   */
  constructor(opts) {
    this.o = opts;
    this.cwd = null;
    this.entries = [];
    this.selected = new Set();
    this.anchor = -1;
    const btn = (label, title, fn) => h("button", { class: "btn ghost", title, onclick: fn }, label);
    this.pathInput = h("input", { class: "sftp-path", spellcheck: "false", title: "Pfad eintippen + Enter" });
    this.pathInput.addEventListener("focus", () => this.pathInput.select());
    this.pathInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { e.preventDefault(); this.load(this.pathInput.value.trim()); }
      if (e.key === "Escape") { this.showPath(); this.list.focus(); }
    });
    this.list = h("div", { class: "sftp-list", tabindex: "0" });
    this.list.addEventListener("keydown", (e) => this.onKey(e));
    this.list.addEventListener("contextmenu", (e) => {
      if (e.target.closest(".sftp-row[data-i]")) return;
      e.preventDefault();
      this.o.onMenu(e.clientX, e.clientY, null);
    });
    this.bindDrop(this.list, () => this.cwd);
    this.el = h("div", { class: "pane pane-" + opts.kind },
      h("div", { class: "sftp-head" },
        h("span", { class: "pane-title" }, opts.title),
        btn("↑", "Übergeordneter Ordner (Backspace)", () => this.up()),
        btn("⌂", opts.kind === "remote" ? "Home-Verzeichnis" : "Start-Ordner (letzter Download-Ordner)", () => opts.home().then((p) => this.load(p)).catch((e) => toast(errMsg(e), "err"))),
        this.pathInput,
        btn(icon("refresh"), "Aktualisieren (F5)", () => this.reload()),
        ...(opts.headExtra || [])),
      opts.tools ? h("div", { class: "sftp-tools" }, opts.tools) : null,
      this.list);
  }

  focus() { this.list.focus(); }
  isRoot() { return this.o.kind === "local" ? this.cwd === "" : this.cwd === "/"; }
  showPath() { this.pathInput.value = this.cwd === "" ? this.o.rootLabel || "" : this.cwd ?? ""; }

  message(text, err = false) {
    this.list.replaceChildren(h("div", { class: "sftp-msg" + (err ? " err" : "") }, text));
  }

  async up() {
    try { this.load(await this.o.parent(this.cwd)); } catch (e) { toast(errMsg(e), "err"); }
  }
  reload() { return this.load(this.cwd ?? ""); }

  async load(path) {
    if (path === this.o.rootLabel) path = "";
    if (!this.entries.length) this.message("Lade…");
    const seq = (this.loadSeq = (this.loadSeq || 0) + 1);
    try {
      const l = await this.o.load(path);
      if (seq !== this.loadSeq) return; // a newer navigation won
      if (l.path !== this.cwd) { this.selected.clear(); this.anchor = -1; }
      this.cwd = l.path;
      this.entries = l.entries || [];
      this.showPath();
      this.render();
      this.o.onChanged?.(this.cwd);
    } catch (e) {
      if (seq !== this.loadSeq) return;
      this.showPath();
      this.message(errMsg(e), true);
    }
  }

  render() {
    const rows = [h("div", { class: "sftp-row hdr" }, h("span"), h("span", {}, "Name"), h("span", { class: "s" }, "Größe"), h("span", { class: "m" }, "Geändert"))];
    if (!this.isRoot()) {
      const up = h("div", { class: "sftp-row", title: "Eine Ebene hoch" }, h("span", { class: "i" }, "↰"), h("span", { class: "n" }, ".."), h("span"), h("span"));
      up.addEventListener("dblclick", () => this.up());
      rows.push(up);
    }
    this.entries.forEach((e, i) => {
      const r = h("div", {
        class: "sftp-row" + (this.selected.has(e.path) ? " selected" : ""),
        title: `${e.mode ? e.mode + "  " : ""}${e.path}`, "data-i": i, draggable: "true",
      },
      h("span", { class: "i" }, e.isDir ? (this.o.kind === "local" && this.cwd === "" ? "💽" : "📁") : e.isLink ? "↪" : "📄"),
      h("span", { class: "n" }, e.name),
      h("span", { class: "s" }, e.isDir ? "" : fmtSize(e.size)),
      h("span", { class: "m" }, fmtDate(e.modTime)));
      r.addEventListener("mousedown", (ev) => { if (ev.button === 0 || !this.selected.has(e.path)) this.clickRow(i, ev); });
      r.addEventListener("dblclick", () => this.open(e));
      r.addEventListener("contextmenu", (ev) => { ev.preventDefault(); this.o.onMenu(ev.clientX, ev.clientY, e); });
      r.addEventListener("dragstart", (ev) => {
        if (!this.selected.has(e.path)) { this.selected = new Set([e.path]); this.paintSelection(); }
        ev.dataTransfer.setData(DRAG_TYPE, JSON.stringify({ kind: this.o.kind, paths: this.selection().map((x) => x.path) }));
        ev.dataTransfer.effectAllowed = "copy";
      });
      if (e.isDir) this.bindDrop(r, () => e.path);
      rows.push(r);
    });
    if (!this.entries.length) rows.push(h("div", { class: "sftp-msg" }, this.o.emptyText));
    this.list.replaceChildren(...rows);
  }

  /** Accept rows dragged from the other pane; target is the folder under the cursor. */
  bindDrop(el, targetDir) {
    el.addEventListener("dragover", (e) => {
      if (!e.dataTransfer.types.includes(DRAG_TYPE)) return;
      e.preventDefault();
      e.stopPropagation();
      el.classList.add("drop");
    });
    el.addEventListener("dragleave", () => el.classList.remove("drop"));
    el.addEventListener("drop", (e) => {
      el.classList.remove("drop");
      const raw = e.dataTransfer.getData(DRAG_TYPE);
      if (!raw) return;
      e.preventDefault();
      e.stopPropagation();
      let data;
      try { data = JSON.parse(raw); } catch { return; }
      if (data.kind !== this.o.kind) this.o.onDropFiles(data, targetDir());
    });
  }

  clickRow(i, ev) {
    const path = this.entries[i].path;
    if (ev.shiftKey && this.anchor >= 0) {
      const [a, b] = [Math.min(this.anchor, i), Math.max(this.anchor, i)];
      if (!ev.ctrlKey) this.selected.clear();
      for (let k = a; k <= b; k++) this.selected.add(this.entries[k].path);
    } else if (ev.ctrlKey) {
      if (this.selected.has(path)) this.selected.delete(path); else this.selected.add(path);
      this.anchor = i;
    } else {
      this.selected = new Set([path]);
      this.anchor = i;
    }
    this.paintSelection();
  }

  // Only toggles classes: re-rendering rows here would break dblclick.
  paintSelection() {
    this.list.querySelectorAll(".sftp-row[data-i]").forEach((r) => {
      r.classList.toggle("selected", this.selected.has(this.entries[+r.dataset.i]?.path));
    });
  }

  selection() { return this.entries.filter((e) => this.selected.has(e.path)); }

  open(e) {
    if (e.isDir) this.load(e.path);
    else this.o.onOpenFile(e);
  }

  onKey(e) {
    const sel = this.selection();
    if (e.key === "Backspace") { e.preventDefault(); this.up(); }
    else if (e.key === "F5") { e.preventDefault(); this.reload(); }
    else if (e.key === "Enter" && sel.length === 1) { e.preventDefault(); this.open(sel[0]); }
    else if (e.key === "a" && e.ctrlKey) { e.preventDefault(); this.selected = new Set(this.entries.map((x) => x.path)); this.paintSelection(); }
    else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!this.entries.length) return;
      const i = Math.max(0, Math.min(this.entries.length - 1, this.anchor + (e.key === "ArrowDown" ? 1 : -1)));
      this.clickRow(i, { shiftKey: e.shiftKey, ctrlKey: false });
      this.list.querySelectorAll(".sftp-row.selected")[0]?.scrollIntoView({ block: "nearest" });
    } else this.o.onKey?.(e, sel);
  }
}

export class SftpPanel {
  constructor(tab, el) {
    this.tab = tab;
    this.el = el;
    this.jobs = new Map();
    this.build();
  }

  get cwd() { return this.remote.cwd ?? ""; }
  get entries() { return this.remote.entries; }

  build() {
    const btn = (label, title, fn, cls = "btn ghost") => h("button", { class: cls, title, onclick: fn }, label);
    this.remote = new FilePane({
      kind: "remote",
      title: "Server",
      load: (p) => this.listRemote(p),
      parent: (p) => remoteParent(p),
      home: () => "",
      onOpenFile: (e) => this.download([e.path]),
      onMenu: (x, y, e) => this.remoteMenu(x, y, e),
      onDropFiles: (d, dir) => this.upload(d.paths, dir),
      onKey: (e, sel) => {
        if (e.key === "Delete" && sel.length) { e.preventDefault(); this.remove(sel); }
        else if (e.key === "F2" && sel.length === 1) { e.preventDefault(); this.rename(sel[0]); }
      },
      emptyText: "Leerer Ordner – Dateien von unten oder aus dem Explorer hierher ziehen.",
      headExtra: [btn("✕", "SFTP schließen (Alt+B)", () => this.tab.toggleSftp())],
      tools: [
        btn("+ Ordner", "Neuen Ordner auf dem Server anlegen", () => this.mkdir()),
        btn("⎘ cd", "Terminal in diesen Ordner wechseln", () => this.cdTerminal(this.cwd)),
        btn("⬆ Datei wählen…", "Dateien per Windows-Dialog hochladen", () => this.pickUpload(false)),
        btn("⬆ Ordner wählen…", "Ordner per Windows-Dialog hochladen", () => this.pickUpload(true)),
      ],
    });
    this.local = new FilePane({
      kind: "local",
      title: "Dieser PC",
      rootLabel: "Dieser PC",
      load: (p) => api.LocalList(p),
      parent: (p) => api.LocalParent(p),
      home: () => api.LocalHome(),
      onOpenFile: (e) => this.upload([e.path]),
      onMenu: (x, y, e) => this.localMenu(x, y, e),
      onDropFiles: (d, dir) => this.download(d.paths, dir),
      onChanged: (dir) => store.set(LOCAL_DIR_KEY, dir),
      emptyText: "Leerer Ordner.",
      headExtra: [btn("📂", "Diesen Ordner im Explorer öffnen", () => this.revealLocal(this.local.cwd))],
    });
    this.transfers = h("div", { class: "transfers" });
    this.xferBar = h("div", { class: "xferbar" },
      btn("⬇ Herunterladen", "Auswahl vom Server in den PC-Ordner unten kopieren (oder Doppelklick / Ziehen)", () => this.download(), "btn"),
      h("span", { class: "xferbar-hint" }, "Doppelklick oder Ziehen überträgt"),
      btn("⬆ Hochladen", "Auswahl vom PC in den Server-Ordner oben kopieren (oder Doppelklick / Ziehen)", () => this.uploadSelection(), "btn"));
    this.el.append(this.remote.el, this.xferBar, this.local.el, this.transfers);

    // Explorer drag feedback; the paths arrive via the Wails file-drop event.
    this.el.addEventListener("dragover", (e) => { if (e.dataTransfer.types.includes("Files")) this.el.classList.add("dragover"); });
    this.el.addEventListener("dragleave", (e) => { if (!this.el.contains(e.relatedTarget)) this.el.classList.remove("dragover"); });
    this.el.addEventListener("drop", () => this.el.classList.remove("dragover"));

    const saved = store.get(LOCAL_DIR_KEY, null); // last PC folder (per viewer)
    if (saved !== null) this.local.load(saved);
    else api.LocalHome().then((d) => this.local.load(d), () => this.local.load(""));
  }

  focus() { this.remote.focus(); }

  /** Tab jumps between server and PC, Esc goes back to the terminal. */
  bindPaneKeys() {
    for (const [from, to] of [[this.remote, this.local], [this.local, this.remote]]) {
      from.list.addEventListener("keydown", (e) => {
        if (e.key === "Tab" && !e.ctrlKey && !e.altKey) { e.preventDefault(); to.focus(); }
        else if (e.key === "Escape") { e.preventDefault(); this.tab.focus(); }
      });
    }
  }

  listRemote(p) {
    if (this.tab.state !== "connected") return Promise.reject(new Error("Nicht verbunden – erst verbinden, dann aktualisieren."));
    return api.SftpList(this.tab.id, p || "");
  }

  /** Called by the tab on (re)connect or when the panel opens. */
  load(path) {
    return this.remote.load(path);
  }

  remoteMenu(x, y, entry) {
    const pane = this.remote;
    const sel = entry ? (pane.selected.has(entry.path) ? pane.selection() : [entry]) : [];
    if (entry && !pane.selected.has(entry.path)) { pane.selected = new Set([entry.path]); pane.paintSelection(); }
    const items = [];
    if (entry) {
      if (entry.isDir && sel.length === 1) items.push({ label: "Öffnen", action: () => pane.load(entry.path), hint: "Enter" });
      items.push({ label: `⬇ ${sel.length > 1 ? sel.length + " Elemente" : "Herunterladen"} nach „${this.localName()}“`, action: () => this.download(sel.map((e) => e.path)) });
      items.push({ label: "Herunterladen nach… (Ordner wählen)", action: () => this.downloadDialog(sel.map((e) => e.path)) });
      items.push({ label: "Umbenennen", hint: "F2", disabled: sel.length !== 1, action: () => this.rename(entry) });
      items.push({ label: "Pfad kopieren", action: () => clipboard.set(sel.map((e) => e.path).join("\n")) });
      if (entry.isDir) items.push({ label: "Im Terminal öffnen (cd)", action: () => this.cdTerminal(entry.path) });
      items.push("-");
      items.push({ label: "Löschen", hint: "Entf", danger: true, action: () => this.remove(sel) });
      items.push("-");
    }
    items.push({ label: "Neuer Ordner…", action: () => this.mkdir() });
    items.push({ label: "Aktualisieren", hint: "F5", action: () => pane.reload() });
    contextMenu(x, y, items);
  }

  localMenu(x, y, entry) {
    const pane = this.local;
    const sel = entry ? (pane.selected.has(entry.path) ? pane.selection() : [entry]) : [];
    if (entry && !pane.selected.has(entry.path)) { pane.selected = new Set([entry.path]); pane.paintSelection(); }
    const items = [];
    if (entry) {
      if (entry.isDir && sel.length === 1) items.push({ label: "Öffnen", action: () => pane.load(entry.path), hint: "Enter" });
      items.push({ label: `⬆ ${sel.length > 1 ? sel.length + " Elemente" : "Hochladen"} nach „${this.cwd || "/"}“`, disabled: pane.cwd === "", action: () => this.upload(sel.map((e) => e.path)) });
      items.push({ label: "Im Explorer zeigen", action: () => this.revealLocal(entry.path) });
      items.push({ label: "Pfad kopieren", action: () => clipboard.set(sel.map((e) => e.path).join("\n")) });
      items.push("-");
    }
    items.push({ label: "Aktualisieren", hint: "F5", action: () => pane.reload() });
    contextMenu(x, y, items);
  }

  localName() {
    const d = this.local.cwd;
    return d ? baseName(d) || d : "Dieser PC";
  }

  revealLocal(path) {
    if (!path) return;
    api.RevealLocal(path).catch((e) => toast(errMsg(e), "err"));
  }

  cdTerminal(path) {
    // Quotes don't stop the TTY from acting on control chars (\r = Enter).
    if (/[\x00-\x1f\x7f]/.test(path)) { toast("Pfad enthält Steuerzeichen – cd aus Sicherheitsgründen nicht ausgeführt.", "err"); return; }
    if (this.tab.state === "connected") api.Write(this.tab.id, `cd ${shellQuote(path)}\r`).catch((e) => toast(errMsg(e), "err"));
    this.tab.focus();
  }

  async mkdir() {
    const name = await inputDialog({ title: "Neuer Ordner auf dem Server", label: "Name", ok: "Anlegen" });
    if (!name) return;
    try { await api.SftpMkdir(this.tab.id, this.cwd, name.trim()); await this.remote.reload(); }
    catch (e) { this.fail("Ordner anlegen", e); }
  }

  async rename(entry) {
    const name = await inputDialog({ title: "Umbenennen", label: "Neuer Name", value: entry.name, ok: "Umbenennen" });
    if (!name || name === entry.name) return;
    try { await api.SftpRename(this.tab.id, entry.path, join(this.cwd, name.trim())); await this.remote.reload(); }
    catch (e) { this.fail("Umbenennen", e); }
  }

  async remove(entries) {
    if (!entries.length) return;
    const dirs = entries.filter((e) => e.isDir).length;
    const what = entries.length === 1 ? `„${entries[0].name}“` : `${entries.length} Elemente`;
    const ok = await confirmDialog({
      title: "Löschen",
      message: `${what} auf dem Server löschen?${dirs ? " Ordner werden mit Inhalt gelöscht." : ""}`,
      ok: "Löschen",
      danger: true,
    });
    if (!ok) return;
    try { await api.SftpRemove(this.tab.id, entries.map((e) => e.path)); }
    catch (e) { this.fail("Löschen", e); }
    this.remote.reload();
  }

  async pickUpload(folder) {
    const paths = await (folder ? api.PickUploadFolder() : api.PickUploadFiles()).catch(() => []);
    if (paths?.length) this.upload(paths);
  }

  uploadSelection() {
    const paths = this.local.selection().map((e) => e.path);
    if (!paths.length) { toast("Unten im PC-Bereich erst Dateien oder Ordner markieren."); return; }
    this.upload(paths);
  }

  async upload(paths, targetDir = this.cwd) {
    if (this.tab.state !== "connected") { this.fail("Hochladen", "Nicht verbunden."); return; }
    if (!paths.length) return;
    if (this.local.cwd === "" && paths.some((p) => /^[A-Z]:\\$/i.test(p))) { this.fail("Hochladen", "Ganze Laufwerke werden nicht hochgeladen."); return; }
    let onExist = "overwrite";
    {
      const listing = targetDir === this.cwd ? { entries: this.entries } : await api.SftpList(this.tab.id, targetDir).catch(() => ({}));
      const existing = new Set((listing.entries || []).map((e) => e.name));
      const clashes = paths.map(baseName).filter((n) => existing.has(n));
      if (clashes.length) {
        onExist = await askOnExist(clashes, targetDir || "/");
        if (!onExist) return;
        if (onExist === "skip" && clashes.length === paths.length) return;
      }
    }
    try { await api.SftpUpload(this.tab.id, targetDir, paths, onExist); }
    catch (e) { this.fail("Hochladen", e); }
  }

  async download(paths, localDir = this.local.cwd) {
    const list = paths || this.remote.selection().map((e) => e.path);
    if (!list.length) { toast("Oben im Server-Bereich erst Dateien oder Ordner markieren."); return; }
    if (!localDir) { this.fail("Herunterladen", "Unten im PC-Bereich zuerst einen Zielordner öffnen (nicht die Laufwerksübersicht)."); return; }
    try { await api.SftpDownloadTo(this.tab.id, list, localDir); }
    catch (e) { this.fail("Herunterladen", e); }
  }

  async downloadDialog(paths) {
    try { await api.SftpDownload(this.tab.id, paths); }
    catch (e) { this.fail("Herunterladen", e); }
  }

  /** Errors stay visible in the transfer list until closed (and as a toast). */
  fail(what, e) {
    const msg = errMsg(e);
    toast(`${what}: ${msg}`, "err", 8000);
    const close = h("button", { class: "btn icon", title: "Meldung schließen" }, "✕");
    const el = h("div", { class: "xfer error" },
      h("div", { class: "xfer-top" }, h("span", { class: "xfer-name" }, `⚠ ${what} fehlgeschlagen`), close),
      h("div", { class: "err" }, msg));
    close.onclick = () => el.remove();
    this.transfers.prepend(el);
  }

  onProgress(p) {
    let job = this.jobs.get(p.jobId);
    if (!job) {
      const name = h("span", { class: "xfer-name" });
      const meta = h("span", { class: "xfer-meta" });
      const bar = h("i");
      const cancel = h("button", { class: "btn icon", title: "Abbrechen", onclick: () => api.CancelTransfer(p.jobId).catch(() => {}) }, "✕");
      const err = h("div", { class: "err" });
      const el = h("div", { class: "xfer" }, h("div", { class: "xfer-top" }, name, meta, cancel), h("div", { class: "bar" }, bar), err);
      job = { el, name, meta, bar, cancel, err, t0: performance.now(), last: { t: performance.now(), done: 0 }, speed: 0 };
      this.jobs.set(p.jobId, job);
      this.transfers.prepend(el);
    }
    const now = performance.now();
    const dt = (now - job.last.t) / 1000;
    if (dt > 0.3) {
      const inst = (p.done - job.last.done) / dt;
      job.speed = job.speed ? job.speed * 0.6 + inst * 0.4 : inst;
      job.last = { t: now, done: p.done };
    }
    const arrow = p.kind === "upload" ? "⬆" : "⬇";
    job.name.textContent = `${arrow} ${p.file ? baseName(p.file) : (p.kind === "upload" ? "Upload" : "Download")}`;
    const pct = p.total ? Math.round((p.done / p.total) * 100) : (p.state === "done" ? 100 : 0);
    job.bar.style.width = pct + "%";
    job.el.className = "xfer " + p.state;
    if (p.state === "running") {
      job.meta.textContent = `${p.filesDone}/${p.files} · ${fmtSize(p.done)} / ${fmtSize(p.total)} · ${fmtSize(Math.max(0, job.speed))}/s`;
      return;
    }
    const secs = Math.max(0.1, (now - job.t0) / 1000);
    if (p.state === "done") {
      job.cancel.remove();
      job.meta.textContent = `${p.files} Datei${p.files === 1 ? "" : "en"} · ${fmtSize(p.total)} · ${fmtSize(p.total / secs)}/s`;
      job.name.textContent = `${arrow} fertig${p.dest ? " → " + p.dest : ""}`;
      job.name.title = p.dest || "";
      if (p.skipped) job.err.textContent = `${p.skipped} übersprungen (Links auf Ordner, Geräte o. Ä.)`;
      if (p.kind === "upload" && p.dest === this.cwd) this.remote.reload();
      if (p.kind === "download" && p.dest === this.local.cwd) this.local.reload();
      setTimeout(() => { job.el.remove(); this.jobs.delete(p.jobId); }, DONE_LINGER_MS);
      return;
    }
    // Error or cancel: keep the entry until the user closes it.
    job.meta.textContent = p.state === "cancelled" ? "abgebrochen" : "Fehler";
    job.err.textContent = p.error || "";
    job.cancel.title = "Meldung schließen";
    job.cancel.onclick = () => { job.el.remove(); this.jobs.delete(p.jobId); };
    if (p.state === "error") toast(`${p.kind === "upload" ? "Hochladen" : "Herunterladen"} fehlgeschlagen: ${p.error || "unbekannt"}`, "err", 8000);
  }
}

/** Asks what to do with names that already exist; resolves overwrite, rename, skip or null. */
function askOnExist(clashes, dir) {
  const what = clashes.length === 1 ? `„${clashes[0]}“ gibt es` : `${clashes.length} Elemente gibt es`;
  return modal((close) => ({
    head: "Schon vorhanden",
    body: h("div", {},
      h("p", {}, `${what} schon in ${dir}.`),
      clashes.length > 1 ? h("p", { class: "hint" }, clashes.slice(0, 8).join(", ") + (clashes.length > 8 ? " …" : "")) : null,
      h("p", { class: "hint" }, "Umbenennen lädt als „Name (1)“ hoch. Ordner werden beim Überschreiben zusammengeführt.")),
    foot: [
      h("button", { class: "btn left", onclick: () => close(null) }, "Abbrechen"),
      h("button", { class: "btn", onclick: () => close("skip") }, "Überspringen"),
      h("button", { class: "btn", onclick: () => close("rename") }, "Umbenennen"),
      h("button", { class: "btn danger", onclick: () => close("overwrite") }, "Überschreiben"),
    ],
  })).then((v) => v || null);
}
