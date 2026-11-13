// A remote desktop tab: same bar entry and pane as a terminal, no terminal.
import { api, errMsg } from "./api.js";
import { contextMenu, h, overlayOpen, toast } from "./ui.js";
import { Tab } from "./tab.js";

/**
 * Opening many RDP sessions at once: their logins (Windows credential and
 * certificate prompts) run one after the other. Otherwise a session that
 * finishes takes the keyboard while you are typing into the next prompt.
 */
const rdpLogins = {
  queue: [],
  busy: null,
  timer: 0,
  enqueue(tab) {
    if (!this.queue.includes(tab) && this.busy !== tab) this.queue.push(tab);
    this.next();
  },
  done(tab) {
    this.queue = this.queue.filter((t) => t !== tab);
    if (this.busy !== tab) return;
    this.busy = null;
    clearTimeout(this.timer);
    this.next();
  },
  next() {
    if (this.busy || !this.queue.length) return;
    const tab = this.queue.shift();
    this.busy = tab;
    // A prompt left open for minutes must not block the others forever.
    this.timer = setTimeout(() => { if (this.busy === tab) { this.busy = null; this.next(); } }, RDP_LOGIN_MAX);
    tab.start();
  },
};
const RDP_LOGIN_MAX = 3 * 60 * 1000;

/**
 * RDP session inside easyTTY (like SecureCRT): the backend lays Windows' own
 * Remote Desktop control as a native window exactly over this pane. Native
 * windows cover HTML, so it hides while the pane is not visible or a dialog
 * or menu is open.
 */
export class RdpTab extends Tab {
  build() {
    this.term = null;
    this.logPath = "";
    this.rdpOpen = false;
    this.hostEl = h("div", { class: "rdp-host" });
    this.overlayMsg = h("div", { class: "rdp-msg" });
    this.overlayBtns = h("div", { class: "rdp-btns" },
      h("button", { class: "btn primary", onclick: () => this.connect() }, "Neu verbinden ⏎"),
      h("button", { class: "btn", onclick: () => this.mgr.close(this.id) }, "Schließen"));
    this.overlay = h("div", { class: "rdp-overlay" }, this.overlayMsg, this.overlayBtns);
    this.view = this.makeView("tabview rdp", this.hostEl, this.overlay);
    this.view.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && (this.state === "closed" || this.state === "error")) { e.preventDefault(); this.connect(); }
    });
    this.overlay.tabIndex = -1;
    this.makeTabEl();
    this.mgr.viewsEl.append(this.view);
    this.ro = new ResizeObserver(() => this.place());
    this.ro.observe(this.hostEl);
    this.onOverlay = () => this.place();
    addEventListener("easytty:overlay", this.onOverlay);
  }

  get protocol() { return "rdp"; }

  subtitle() {
    const s = this.session;
    return `RDP ${s.user ? s.user + "@" : ""}${s.host}${s.port && s.port !== 3389 ? ":" + s.port : ""}`;
  }

  /** The pane in physical pixels of the window's client area. */
  rect() {
    const r = this.hostEl.getBoundingClientRect();
    const d = devicePixelRatio || 1;
    return { x: Math.round(r.left * d), y: Math.round(r.top * d), w: Math.round(r.width * d), h: Math.round(r.height * d) };
  }

  /** Moves the native view over the pane, or hides it. */
  place() {
    if (!this.rdpOpen) return;
    const r = this.rect();
    const live = this.state === "connected" || this.state === "connecting";
    const visible = live && this.view.classList.contains("active") && !overlayOpen() && r.w > 0 && r.h > 0;
    api.RDPMove(this.id, r, visible).catch(() => {});
    if (visible && (r.w !== this.lastW || r.h !== this.lastH)) {
      this.lastW = r.w;
      this.lastH = r.h;
      clearTimeout(this.fitTimer); // the remote resolution follows once resizing settles
      this.fitTimer = setTimeout(() => api.RDPFit(this.id).catch(() => {}), 400);
    }
  }

  refit() { this.place(); }

  focus() {
    if (this.state === "connected") api.RDPFocus(this.id).catch(() => {});
    else this.overlay.focus();
  }

  /** Queues the connect: only one RDP login runs at a time (see rdpLogins). */
  connect() {
    if (this.state === "queued" || this.state === "connecting") return;
    this.onState({ state: "queued" });
    rdpLogins.enqueue(this);
  }

  /** Called by rdpLogins when it is this tab's turn. */
  async start() {
    this.onState({ state: "connecting", message: this.session.host });
    try { // rect() forces the layout itself; no animation frame (none come while minimised)
      await api.RDPOpen(this.id, this.session.id, this.rect());
      // Closed while the login was still running: the view exists only now.
      if (this.disposed) { api.RDPClose(this.id).catch(() => {}); return; }
      this.rdpOpen = true;
      this.lastW = this.lastH = 0;
      this.place();
    } catch (e) {
      this.onState({ state: "error", message: errMsg(e) });
    }
  }

  onState(st) {
    this.state = st.state;
    this.message = st.message || "";
    const dot = st.state === "queued" ? "connecting" : st.state;
    this.tabEl.querySelector(".state").className = "state " + dot;
    this.paneLabel.querySelector(".state").className = "state " + dot;
    const failed = st.state === "closed" || st.state === "error";
    this.overlay.classList.toggle("open", st.state !== "connected");
    this.overlayBtns.hidden = !failed;
    const target = this.subtitle().slice(4);
    this.overlayMsg.textContent = st.state === "queued" ? `Wartet, bis die vorige Anmeldung fertig ist – dann ${target}`
      : st.state === "connecting" ? `Verbinde mit ${target} …`
        : failed ? (this.message || "Verbindung getrennt") : "";
    if (st.state !== "queued" && st.state !== "connecting") rdpLogins.done(this);
    this.place();
    // A finished login must not grab the keyboard from the next one: only the tab you look at gets it.
    if (st.state === "connected" && this.mgr.active === this.id) this.focus();
    this.mgr.changed();
  }

  menu(x, y) {
    contextMenu(x, y, [
      { label: "Neu verbinden", action: () => this.connect(), disabled: this.state === "connecting" },
      { label: "Duplizieren", hint: "Alt+D", action: () => this.mgr.duplicate(this.id) },
      ...(this.groupId ? [{ label: "Aus der Ansicht lösen (eigener Tab)", action: () => this.mgr.detach(this.id) }] : []),
      { label: "In eigenem Remotedesktop-Fenster öffnen", action: () => api.ConnectRDP(this.session.id).catch((e) => toast(errMsg(e), "err")) },
      "-",
      { label: "Schließen", hint: "Alt+W", danger: true, action: () => this.mgr.close(this.id) },
    ]);
  }

  dispose() {
    this.disposed = true;
    this.rdpOpen = false;
    clearTimeout(this.fitTimer);
    rdpLogins.done(this);
    api.RDPClose(this.id).catch(() => {});
    removeEventListener("easytty:overlay", this.onOverlay);
    this.subs.forEach((off) => off && off());
    this.ro.disconnect();
    this.view.remove();
    this.tabEl.remove();
  }

  // Terminal features do not apply to a remote desktop.
  openSearch() {}
  closeSearch() {}
  toggleSftp() { toast("SFTP gibt es nur bei SSH-Verbindungen."); }
  openSftp() {}
  applySettings() {}
  zoom() {}
  copy() {}
  paste() {}
  pasteText() {}
  setAI() {}
  revealLog() {}
}

