// One terminal tab: an xterm.js instance wired to a backend connection.
import { api, b64ToBytes, clipboard, errMsg, events, openURL } from "./api.js";
import { closeX, contextMenu, h, makeSplitter, modal, toast } from "./ui.js";
import { keyAction } from "./keys.js";
import { editPaste } from "./pasteDialog.js";
import { openConfigDiff } from "./configDiff.js";
import { Highlighter } from "./highlight.js";
import { SftpPanel } from "./sftp.js";

/** Drag data of a tab or tile; the value is the tab id. */
export const TAB_DRAG = "application/x-easytty-tab";

const THEMES = {
  dark: {
    background: "#0d0f14", foreground: "#d8dee9", cursor: "#4fb3ff", cursorAccent: "#0d0f14",
    selectionBackground: "rgba(79,179,255,0.32)",
    // ls colours must stay apart: executables green, links cyan, folders blue, text white.
    // Hues are spread on purpose: grass green ~105°, sky cyan ~190°, indigo blue ~228°.
    black: "#1b1f27", red: "#ff5d6c", green: "#5cc845", yellow: "#f2b84b", blue: "#5b7bff", magenta: "#d07cf0", cyan: "#22b8e6", white: "#d8dee9",
    brightBlack: "#6b7383", brightRed: "#ff7b86", brightGreen: "#8be06a", brightYellow: "#ffd27a", brightBlue: "#6f84ff", brightMagenta: "#e4a6ff", brightCyan: "#4fd8f5", brightWhite: "#ffffff",
  },
  // Catppuccin Latte: soft grey-blue instead of pure white, the most popular gentle light palette.
  light: {
    background: "#eff1f5", foreground: "#4c4f69", cursor: "#1e66f5", cursorAccent: "#eff1f5",
    selectionBackground: "rgba(30,102,245,0.20)",
    black: "#5c5f77", red: "#d20f39", green: "#40a02b", yellow: "#df8e1d", blue: "#1e66f5", magenta: "#ea76cb", cyan: "#179299", white: "#acb0be",
    brightBlack: "#6c6f85", brightRed: "#de293e", brightGreen: "#49af3d", brightYellow: "#eea02d", brightBlue: "#456eff", brightMagenta: "#fe85d8", brightCyan: "#2d9fa8", brightWhite: "#bcc0cc",
  },
};

const RESIZE_DEBOUNCE = 40;
const ACK_DELAY = 20;
const GRAY = "\x1b[90m";
const RED = "\x1b[31m";
const RESET = "\x1b[0m";

let tabSeq = 0;
const newTabId = () => `t${Date.now().toString(36)}${(tabSeq++).toString(36)}`;

export class Tab {
  constructor(mgr, opts) {
    this.mgr = mgr;
    this.id = opts.id || newTabId(); // the backend picks the id when an AI run opens the tab
    this.session = opts.session || null;
    this.target = opts.target || "";
    this.sftpOnConnect = !!opts.sftp; // "Verbinden mit SFTP-Browser"
    this.groupId = opts.group || null; // window layout this tab belongs to
    this.slot = opts.slot ?? 0;
    this.title = this.session ? this.session.name : this.target;
    this.state = "new";
    this.message = "";
    this.sftp = null;
    this.fontDelta = 0;
    this.build();
    this.pendingAck = 0;
    this.decoder = new TextDecoder("utf-8"); // streaming: a character may span two chunks
    this.hl = new Highlighter(mgr.settings.highlights);
    this.subs = [events.on("term:data:" + this.id, (b64) => {
      const bytes = b64ToBytes(b64);
      // The backend counts bytes for flow control, whatever is written here.
      if (this.hl.active) this.term.write(this.hl.push(this.decoder.decode(bytes, { stream: true })), () => this.ack(bytes.length));
      else this.term.write(bytes, () => this.ack(bytes.length));
    })];
  }

  // Flow control: tell the backend what has been rendered, batched to ~1 call per 20 ms.
  ack(n) {
    this.pendingAck += n;
    if (this.ackTimer) return;
    this.ackTimer = setTimeout(() => {
      this.ackTimer = 0;
      const sent = this.pendingAck;
      this.pendingAck = 0;
      api.Ack(this.id, sent).catch(() => {});
    }, ACK_DELAY);
  }

  get protocol() { return this.session ? this.session.protocol : "ssh"; }

  build() {
    const s = this.mgr.settings;
    this.term = new Terminal({
      fontFamily: s.fontFamily,
      fontSize: s.fontSize,
      scrollback: s.scrollback,
      cursorBlink: s.cursorBlink,
      allowProposedApi: true,
      macOptionIsMeta: true,
      rightClickSelectsWord: false,
      theme: THEMES[s.theme] || THEMES.dark,
    });
    this.fit = new FitAddon.FitAddon();
    this.search = new SearchAddon.SearchAddon();
    this.term.loadAddon(this.fit);
    this.term.loadAddon(this.search);
    this.term.loadAddon(new WebLinksAddon.WebLinksAddon((e, uri) => { if (e.ctrlKey) openURL(uri); }));
    const uni = new Unicode11Addon.Unicode11Addon();
    this.term.loadAddon(uni);
    this.term.unicode.activeVersion = "11";

    this.termEl = h("div", { class: "term" });
    this.searchInput = h("input", { placeholder: "Suchen…", spellcheck: "false" });
    this.searchBar = h("div", { class: "searchbar" },
      this.searchInput,
      h("button", { class: "btn icon", title: "Vorheriger (Shift+Enter)", onclick: () => this.find(-1) }, "↑"),
      h("button", { class: "btn icon", title: "Nächster (Enter)", onclick: () => this.find(1) }, "↓"),
      h("button", { class: "btn icon", title: "Schließen (Esc)", onclick: () => this.closeSearch() }, "✕"));
    this.searchInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { e.preventDefault(); this.find(e.shiftKey ? -1 : 1); }
      if (e.key === "Escape") { e.preventDefault(); this.closeSearch(); }
    });
    this.searchInput.addEventListener("input", () => this.find(1, true));

    this.reconnectMsg = h("span", {});
    this.reconnectBar = h("div", { class: "reconnect" }, this.reconnectMsg,
      h("button", { class: "btn primary", onclick: () => this.connect() }, "Neu verbinden ⏎"),
      h("button", { class: "btn", onclick: () => this.mgr.close(this.id) }, "Tab schließen"),
      closeX("Hinweis ausblenden (Enter verbindet trotzdem neu)", () => this.reconnectBar.classList.remove("open")));

    this.pasteMsg = h("span", {});
    this.pasteFill = h("i");
    this.pasteBar = h("div", { class: "reconnect pastebar" },
      h("div", { class: "pastebar-text" }, this.pasteMsg, h("div", { class: "bar" }, this.pasteFill)),
      h("button", { class: "btn", title: "Zur aktuellen Zeile, an der gerade eingefügt wird", onclick: () => { this.term.scrollToBottom(); this.focus(); } }, "⤓ Nach unten"),
      h("button", { class: "btn", title: "Esc", onclick: () => api.CancelPaste(this.id).catch(() => {}) }, "Abbrechen"),
      closeX("Ausblenden, das Einfügen läuft weiter", () => { this.pasteBarHidden = true; this.pasteBar.classList.remove("open"); }));

    this.termWrap = h("div", { class: "term-wrap" }, this.termEl, this.searchBar, this.reconnectBar, this.pasteBar);
    this.sftpSplit = h("div", { class: "sftp-split" });
    this.sftpEl = h("div", { class: "sftp" });
    this.view = this.makeView("tabview", this.termWrap, this.sftpSplit, this.sftpEl);
    makeSplitter(this.sftpSplit, this.sftpEl, { sign: -1, min: 280, max: 1100, storageKey: "easytty.sftpWidth", onEnd: () => this.refit() });

    this.makeTabEl();
    this.mgr.viewsEl.append(this.view);
    this.term.open(this.termEl);
    this.loadWebgl();
    this.wireTerminal();
  }

  /** The pane (label + content) shown in the workspace or a layout tile. */
  makeView(cls, ...content) {
    this.paneLabel = h("div", { class: "pane-label" },
      h("span", { class: "state" }), h("span", { class: "pane-name" }, this.title));
    const view = h("div", { class: cls }, this.paneLabel, ...content);
    if (this.groupId) view.style.gridArea = "s" + this.slot;
    if (this.session?.color) this.paneLabel.style.borderTopColor = this.session.color;
    this.paneLabel.addEventListener("contextmenu", (e) => { e.preventDefault(); this.menu(e.clientX, e.clientY); });
    // The label is the handle to move a tile within its layout.
    this.paneLabel.draggable = true;
    this.paneLabel.addEventListener("dragstart", (e) => e.dataTransfer.setData(TAB_DRAG, this.id));
    // In a tile layout a click into a tile makes it the active one.
    view.addEventListener("mousedown", () => { if (this.mgr.active !== this.id) this.mgr.activate(this.id); }, true);
    return view;
  }

  /** The entry in the tab bar. */
  makeTabEl() {
    this.tabEl = h("div", { class: "tab", draggable: "true", title: this.subtitle() },
      h("span", { class: "state" }),
      this.folderEl = h("span", { class: "tab-folder" }),
      h("span", { class: "title" }, this.title),
      h("button", { class: "close", title: "Schließen (Alt+W)", onclick: (e) => { e.stopPropagation(); this.mgr.close(this.id); } }, "×"));
    if (this.session?.color) this.tabEl.style.boxShadow = `inset 0 2px 0 ${this.session.color}`;
    this.tabEl.addEventListener("mousedown", (e) => { if (e.button === 1) { e.preventDefault(); this.mgr.close(this.id); } });
    this.tabEl.addEventListener("click", () => this.mgr.activate(this.id));
    this.tabEl.addEventListener("dblclick", () => this.mgr.duplicate(this.id));
    this.tabEl.addEventListener("contextmenu", (e) => { e.preventDefault(); this.menu(e.clientX, e.clientY); });
  }

  loadWebgl() {
    try {
      const gl = new WebglAddon.WebglAddon();
      gl.onContextLoss(() => gl.dispose());
      this.term.loadAddon(gl);
    } catch (e) {
      console.warn("WebGL nicht verfügbar, nutze DOM-Renderer", e);
    }
  }

  wireTerminal() {
    const t = this.term;
    t.attachCustomKeyEventHandler((e) => {
      if (this.pasting) { // keystrokes would interleave with the pasted lines
        if (e.type === "keydown" && e.key === "Escape") api.CancelPaste(this.id).catch(() => {});
        return false;
      }
      // App shortcuts are handled by the document listener; the terminal must not get them.
      if (keyAction(e, { altKeys: this.mgr.settings.altShortcuts !== false })) return false;
      // Strg+V is a key for the device, as in PuTTY; pasting is Alt+V or a right click.
      if (e.type === "keydown" && e.ctrlKey && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "v") {
        e.preventDefault(); // no browser paste event
        if (this.state === "connected") api.Write(this.id, "\x16").catch(() => {});
        return false;
      }
      if (e.type === "keydown" && e.key === "Enter" && !e.ctrlKey && !e.altKey && this.state !== "connected" && this.state !== "connecting") {
        this.connect();
        return false;
      }
      return true;
    });
    t.onData((d) => { if (this.state === "connected") api.Write(this.id, d).catch(() => {}); });
    t.onResize(({ cols, rows }) => { if (this.state === "connected") api.Resize(this.id, cols, rows).catch(() => {}); this.mgr.updateStatus(); });
    t.onSelectionChange(() => {
      // Search highlights matches via the selection; that must not hit the clipboard.
      if (this.mgr.settings.copyOnSelect && t.hasSelection() && !this.searchBar.classList.contains("open")) clipboard.set(t.getSelection());
    });
    t.onTitleChange((title) => { if (title && !this.session) this.setTitle(title); });
    // Other browser paste paths (Shift+Einfg …) are not shortcuts any more: one way, Alt+V.
    this.termEl.addEventListener("paste", (e) => {
      e.preventDefault();
      e.stopPropagation();
      toast("Einfügen: Alt+V oder Rechtsklick");
    }, true);
    this.termEl.addEventListener("contextmenu", (e) => {
      e.preventDefault();
      if (!this.mgr.settings.rightClickPaste) return;
      if (t.hasSelection() && !this.mgr.settings.copyOnSelect) { this.copy(); return; }
      this.paste();
    });
    let timer = 0;
    this.ro = new ResizeObserver(() => { clearTimeout(timer); timer = setTimeout(() => this.refit(), RESIZE_DEBOUNCE); });
    this.ro.observe(this.termWrap);
  }

  subtitle() {
    const s = this.session;
    if (!s) return "SSH " + this.target;
    if (s.protocol === "serial") return `Serial ${s.serialPort} @ ${s.baud}`;
    return `SSH ${s.user ? s.user + "@" : ""}${s.host}:${s.port}`;
  }

  /** Moves this tab into a tile of a view (gid null = plain tab). */
  setGroup(gid, slot = 0) {
    this.groupId = gid;
    this.slot = slot;
    this.view.style.gridArea = gid ? "s" + slot : "";
  }

  menu(x, y) {
    contextMenu(x, y, [
      { label: "Neu verbinden", action: () => this.connect(), disabled: this.state === "connecting" },
      ...(this.session ? [] : [{ label: "💾 Als Session speichern…", action: () => this.mgr.onSaveSession?.(this) }]),
      { label: "Duplizieren", hint: "Alt+D", action: () => this.mgr.duplicate(this.id) },
      ...(this.protocol === "serial" && this.session && (this.session.user || this.session.hasPassword)
        ? [{ label: "Anmelden (Benutzer + Passwort senden)", disabled: this.state !== "connected", action: () => api.SerialLogin(this.id).catch((e) => toast(errMsg(e), "err")) }]
        : []),
      ...(this.groupId ? [{ label: "Aus der Ansicht lösen (eigener Tab)", action: () => this.mgr.detach(this.id) }] : []),
      ...(this.protocol === "serial"
        ? [{ label: "Break senden", disabled: this.state !== "connected", action: () => api.SendBreak(this.id).catch((e) => toast(errMsg(e), "err")) }]
        : []),
      { label: "SFTP-Browser", hint: "Alt+B", action: () => this.toggleSftp(), disabled: this.protocol !== "ssh" },
      { label: "Running-Config sichern", disabled: this.state !== "connected" || this.pasting, action: () => this.saveRunningConfig() },
      { label: "Configs vergleichen…", action: () => openConfigDiff() },
      "-",
      { label: "Log dieser Sitzung zeigen", hint: "Alt+L", disabled: !this.logPath, action: () => this.revealLog() },
      { label: "Einfüge-Bericht (letztes Einfügen)", disabled: !this.lastPaste, action: () => this.showPasteReport() },
      { label: "Log-Ordner öffnen", action: () => api.OpenLogDir().catch((e) => toast(errMsg(e), "err")) },
      "-",
      { label: "Schließen", hint: "Alt+W", danger: true, action: () => this.mgr.close(this.id) },
    ]);
  }

  revealLog() {
    if (this.logPath) api.RevealLog(this.logPath).catch((e) => toast(errMsg(e), "err"));
  }

  /** A quick-connect tab becomes a saved session (title, colour, reconnect use it). */
  adoptSession(session) {
    this.session = session;
    this.setTitle(session.name);
    this.tabEl.title = this.subtitle() + (this.logPath ? `\nLog: ${this.logPath}` : "");
    this.tabEl.style.boxShadow = session.color ? `inset 0 2px 0 ${session.color}` : "";
    this.mgr.changed();
  }

  /** Banner while an AI client types into this tab (null = gone). */
  setAI(client) {
    this.aiBar?.remove();
    this.aiBar = null;
    this.view.classList.toggle("ai-active", !!client);
    this.tabEl.classList.toggle("ai-active", !!client);
    this.mgr.updateTabbar();
    if (!client) return;
    this.aiBar = h("div", { class: "ai-bar" },
      h("span", { class: "ai-dot" }), h("b", {}, client), " tippt gerade Befehle in diesen Tab – bitte nicht dazwischentippen.",
      h("button", { class: "btn small", onclick: () => api.CancelAI(this.id).catch(() => {}) }, "Abbrechen"));
    this.termWrap.prepend(this.aiBar);
  }

  /** Quietly names the session folder in front of the tab title (full path in the tooltip). */
  renderFolder() {
    const path = this.session ? this.mgr.folderPath(this.session.folderId) : [];
    this.folderEl.textContent = path.length ? path.at(-1) + " ›" : "";
    this.folderEl.hidden = !path.length;
    const full = this.session ? (path.length ? path.join(" / ") : "oberste Ebene") : "Quick Connect (nicht gespeichert)";
    this.tabEl.title = `${full} › ${this.title}\n${this.subtitle()}`;
  }

  setTitle(title) {
    this.title = title;
    this.tabEl.querySelector(".title").textContent = title;
    this.paneLabel.querySelector(".pane-name").textContent = title;
    this.mgr.updateTabbar(); // a view's block is named after its sessions
  }

  refit() {
    if (!this.view.classList.contains("active")) return;
    try { this.fit.fit(); } catch { /* not visible yet */ }
  }

  focus() { this.term.focus(); }

  async connect() {
    this.refit();
    const { cols, rows } = this.term;
    this.reconnectBar.classList.remove("open");
    try {
      api.TrackTab(this.id, this.title, this.session?.id || "").catch(() => {});
      if (this.session) await api.Connect(this.id, this.session.id, cols, rows);
      else this.quickTarget = await api.QuickConnect(this.id, this.target, cols, rows);
    } catch (e) {
      this.onState({ state: "error", message: errMsg(e) });
    }
  }

  onState(st) {
    const prev = this.state;
    this.state = st.state;
    this.message = st.message || "";
    const dot = this.tabEl.querySelector(".state");
    dot.className = "state " + st.state;
    this.paneLabel.querySelector(".state").className = "state " + st.state;
    if (st.state === "connecting") {
      this.logPath = ""; // a reconnect gets its own log file
      if (prev !== "new") this.term.write("\r\n");
      this.term.write(`${GRAY}Verbinde mit ${st.message}…${RESET}\r\n`);
    } else if (st.state === "connected") {
      const { cols, rows } = this.term;
      api.Resize(this.id, cols, rows).catch(() => {});
      if (!this.session && !this.saveHintShown) {
        this.saveHintShown = true;
        toast("Quick Connect – mit „💾 Speichern“ oben rechts als Session ablegen.", "info", 6000);
      }
      if (this.sftpOnConnect) { this.sftpOnConnect = false; this.openSftp(); }
      else if (this.sftp && this.sftpOpen) this.sftp.load(this.sftp.cwd);
    } else if (st.state === "closed" || st.state === "error") {
      this.pasting = false; // a paste cut by the disconnect must not keep the keyboard blocked
      this.pasteBar.classList.remove("open");
      const color = st.state === "error" ? RED : GRAY;
      this.term.write(`\r\n${color}${st.state === "error" ? "Fehler: " : ""}${this.message}${RESET}\r\n`);
      this.reconnectMsg.textContent = st.state === "error" ? "Verbindung fehlgeschlagen" : "Verbindung getrennt";
      this.reconnectBar.classList.add("open");
    }
    this.mgr.changed();
  }

  copy() {
    if (this.term.hasSelection()) clipboard.set(this.term.getSelection());
  }

  async paste() {
    const text = await clipboard.get().catch(() => "");
    this.pasteText(text);
  }

  /**
   * One line, or a program that understands bracketed paste (bash, vim):
   * hand it over at once. Otherwise (Cisco IOS & co.) the backend sends it
   * line by line and waits for each echo, so big configs arrive intact.
   */
  async pasteText(text) {
    // Control characters in the clipboard could end bracketed paste early or send Ctrl+C/Ctrl+D.
    text = (text || "").replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g, "");
    if (!text || this.state !== "connected" || this.pasting || this.pasteDialog) return;
    const multi = text.replace(/\r\n?/g, "\n").replace(/\n$/, "").includes("\n");
    if (!multi) { this.term.paste(text); return; }
    const fastDefault = !!this.term.modes.bracketedPasteMode;
    let mode = fastDefault ? "fast" : "paced";
    let delay = -1; // backend default from the settings
    if (this.mgr.settings.pasteConfirm) {
      this.pasteDialog = true;
      let res;
      try { res = await editPaste(text, this.title, { fastDefault, delay: this.mgr.settings.pasteLineDelay || 0 }); } finally { this.pasteDialog = false; }
      this.focus();
      if (!res || this.state !== "connected") return;
      ({ text, mode, delay } = res);
    }
    if (mode === "fast") { this.term.paste(text); return; }
    if (mode === "file") {
      toast("Befehle laufen, die Ausgabe kommt in eine Datei …");
      await this.toFile(() => api.RunSnippetToFile(this.id, "Ausgabe", text), "Ausgabe gespeichert");
      return;
    }
    const total = text.split("\n").length;
    try {
      this.pasting = true;
      this.pasteBarHidden = false;
      this.showPasteBar({ done: 0, total, timeouts: 0 });
      await api.Paste(this.id, text, delay);
    } catch (e) {
      this.pasting = false;
      this.pasteBar.classList.remove("open");
      toast(errMsg(e), "err");
    }
  }

  /**
   * Reads "show running-config" in this tab (works over a bare console too)
   * and stores it in the config folder.
   */
  async saveRunningConfig() {
    toast("Lese die Running-Config … (Seiten blättert easyTTY selbst um)");
    await this.toFile(() => api.SaveRunningConfig(this.id), "Running-Config gespeichert");
  }

  /** Runs a snippet command by command and saves the output as a text file. */
  async runSnippetToFile(sn) {
    if (this.state !== "connected") { toast("Tab ist nicht verbunden.", "err"); return; }
    if (this.pasting) { toast("In diesem Tab läuft gerade ein Einfügen.", "err"); return; }
    toast(`Schnipsel „${sn.name}“ läuft, die Ausgabe kommt in eine Datei …`);
    await this.toFile(() => api.RunSnippetToFile(this.id, sn.name, sn.text), "Ausgabe gespeichert");
  }

  /** Runs save() and offers to open or show the written file. */
  async toFile(save, done) {
    try {
      const path = await save();
      const fail = (e) => toast(errMsg(e), "err");
      toast(`${done}: ${path}`, "ok", 15000, [
        { label: "Öffnen", action: () => api.OpenConfigFile(path).catch(fail) },
        { label: "Im Ordner zeigen", action: () => api.RevealLocal(path).catch(fail) },
        { label: "Vergleichen", action: () => openConfigDiff(path) },
      ]);
    } catch (e) { toast(errMsg(e), "err", 9000); }
    this.focus();
  }

  /** Types a snippet: one line goes out with Enter, several like a paste. */
  async sendSnippet(text) {
    if (this.state !== "connected") { toast("Tab ist nicht verbunden.", "err"); return; }
    if (this.pasting) { toast("In diesem Tab läuft gerade ein Einfügen.", "err"); return; }
    const clean = text.replace(/\r\n?/g, "\n").replace(/\n+$/, "");
    if (clean.includes("\n")) { await this.pasteText(clean + "\n"); return; }
    try { await api.Write(this.id, clean + "\r"); } catch (e) { toast(errMsg(e), "err"); }
    this.focus();
  }

  showPasteBar(p) {
    const pct = p.total ? Math.round((p.done / p.total) * 100) : 0;
    this.pasteMsg.textContent = `Einfügen: ${p.done} / ${p.total} Zeilen${p.timeouts ? ` · ${p.timeouts} ohne Echo` : ""}`;
    this.pasteFill.style.width = pct + "%";
    if (!this.pasteBarHidden) this.pasteBar.classList.add("open");
  }

  onPasteProgress(p) {
    if (p.state === "running") { this.showPasteBar(p); return; }
    this.pasting = false;
    this.pasteBar.classList.remove("open");
    this.lastPaste = { ...p, at: new Date() };
    const where = p.stopLine ? ` – ab Zeile ${p.stopLine} nicht mehr gesendet: „${p.stopText}“` : "";
    const more = " Details: Rechtsklick auf den Tab → Einfüge-Bericht (steht auch im Log).";
    if (p.state === "done") {
      toast(`${p.total} Zeilen eingefügt${p.timeouts ? ` – ${p.timeouts} davon ohne bestätigtes Echo, bitte prüfen.${more}` : ", alle bestätigt."}`, p.timeouts ? "info" : "ok", p.timeouts ? 9000 : 4000);
    } else if (p.state === "cancelled") {
      toast(`Einfügen abgebrochen nach ${p.done} von ${p.total} Zeilen${where}.${more}`, "info", 10000);
    } else {
      toast(`Einfügen fehlgeschlagen nach ${p.done} von ${p.total} Zeilen: ${p.error || "unbekannt"}${where}.${more}`, "err", 12000);
    }
    this.mgr.renderTools();
  }

  /** Report of the last paste: where it stopped and which lines had no echo. */
  showPasteReport() {
    const p = this.lastPaste;
    if (!p) { toast("In diesem Tab wurde noch nichts zeilenweise eingefügt."); return; }
    const states = { done: "fertig", cancelled: "abgebrochen (Esc)", error: "fehlgeschlagen" };
    const row = (k, v) => h("div", { class: "report-row" }, h("span", { class: "muted" }, k), h("span", {}, v));
    modal((close) => ({
      head: "Einfüge-Bericht",
      body: h("div", { class: "report" },
        row("Zeitpunkt", p.at.toLocaleString("de-DE")),
        row("Ergebnis", `${states[p.state] || p.state} – ${p.done} von ${p.total} Zeilen gesendet`),
        p.error ? row("Fehler", p.error) : null,
        p.stopLine ? row("Abgebrochen bei", `Zeile ${p.stopLine}: ${p.stopText}`) : null,
        row("Ohne Echo", p.timeouts ? `${p.timeouts} Zeile(n): ${p.misses?.join(", ") || "–"}${p.timeouts > (p.misses?.length || 0) ? " …" : ""}` : "keine – alle Zeilen bestätigt"),
        h("p", { class: "hint" }, "„Ohne Echo“ heißt: Das Gerät hat die Zeile nicht innerhalb von 3 s zurückgeschickt – z. B. weil ein Befehl lange lief oder eine Rückfrage kam. Diese Zeilen im Terminal bzw. im Log prüfen."),
        this.logPath ? h("button", { class: "btn", onclick: () => this.revealLog() }, "📄 Log dieser Sitzung zeigen") : null),
      foot: [h("button", { class: "btn primary", onclick: () => close() }, "Schließen")],
    }));
  }

  openSearch() {
    this.searchBar.classList.add("open");
    const sel = this.term.getSelection();
    if (sel && !sel.includes("\n")) this.searchInput.value = sel;
    this.searchInput.focus();
    this.searchInput.select();
  }

  closeSearch() {
    this.searchBar.classList.remove("open");
    this.search.clearDecorations?.();
    this.term.clearSelection();
    this.focus();
  }

  find(dir, incremental = false) {
    const q = this.searchInput.value;
    if (!q) return;
    const opts = { caseSensitive: false, incremental };
    if (dir > 0) this.search.findNext(q, opts); else this.search.findPrevious(q, opts);
  }

  zoom(delta) {
    this.fontDelta = delta === 0 ? 0 : this.fontDelta + delta;
    this.term.options.fontSize = Math.max(8, Math.min(40, this.mgr.settings.fontSize + this.fontDelta));
    this.refit();
  }

  get sftpOpen() { return this.view.classList.contains("sftp-open"); }

  toggleSftp() {
    if (this.protocol !== "ssh") { toast("SFTP gibt es nur bei SSH-Verbindungen."); return; }
    if (!this.sftp) { this.sftp = new SftpPanel(this, this.sftpEl); this.sftp.bindPaneKeys(); }
    const open = this.view.classList.toggle("sftp-open");
    if (open) { if (this.state === "connected") this.sftp.load(this.sftp.cwd); this.sftp.focus(); } else this.focus();
    setTimeout(() => this.refit(), 0);
    this.mgr.renderTools();
  }

  /** Alt+B: open SFTP (and jump in) → jump in → close when already inside. */
  sftpShortcut() {
    if (!this.view.classList.contains("sftp-open")) return this.toggleSftp();
    if (this.sftpEl.contains(document.activeElement)) return this.toggleSftp();
    this.sftp.focus();
  }

  openSftp() {
    if (!this.sftpOpen) this.toggleSftp();
  }

  applySettings(s) {
    const o = this.term.options;
    o.fontFamily = s.fontFamily;
    o.fontSize = s.fontSize + this.fontDelta;
    o.scrollback = s.scrollback;
    o.cursorBlink = s.cursorBlink;
    o.theme = THEMES[s.theme] || THEMES.dark;
    this.hl.setRules(s.highlights);
    this.refit();
  }

  dispose() {
    api.CloseTab(this.id).catch(() => {});
    this.subs.forEach((off) => off && off());
    this.ro.disconnect();
    this.term.dispose();
    this.view.remove();
    this.tabEl.remove();
  }
}

