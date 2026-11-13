// Right sidebar: live status of the device behind the active SSH tab
// (load/CPU, memory, disk, top processes), polled while visible.
import { api, errMsg } from "./api.js";
import { fmtSize, h, store } from "./ui.js";

const POLL_MS = 5000;
const OPEN_KEY = "easytty.statusOpen";

const pct = (used, total) => (total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0);

function fmtUptime(sec) {
  const d = Math.floor(sec / 86400), hrs = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60);
  return d ? `${d} T ${hrs} h` : hrs ? `${hrs} h ${m} min` : `${m} min`;
}

function bar(label, value, detail) {
  const level = value >= 90 ? "crit" : value >= 70 ? "warn" : "";
  return h("div", { class: "st-metric" },
    h("div", { class: "st-row" }, h("span", {}, label), h("b", {}, `${value} %`)),
    h("div", { class: "bar " + level }, h("i", { style: { width: value + "%" } })),
    detail ? h("div", { class: "st-detail" }, detail) : null);
}

export class StatusPanel {
  constructor(el, tabs) {
    this.el = el;
    this.tabs = tabs;
    this.timer = 0;
    this.tabId = null;
    this.busy = false;
    this.last = new Map(); // tabId -> snapshot (instant redraw when switching tabs)
    this.polled = new Map(); // tabId -> time of the last query
    this.setOpen(!!store.get(OPEN_KEY, false));
  }

  get open() { return !this.el.classList.contains("hidden"); }

  setOpen(open) {
    this.el.classList.toggle("hidden", !open);
    store.set(OPEN_KEY, open);
    this.refresh();
    requestAnimationFrame(() => this.tabs.current()?.refit());
    this.tabs.renderTools();
  }

  toggle() { this.setOpen(!this.open); }

  /** Called when the active tab or its state changes. */
  refresh() {
    clearTimeout(this.timer);
    if (!this.open) return;
    const tab = this.tabs.current();
    this.tabId = tab?.id || null;
    if (!tab) return this.message("Keine Verbindung offen.");
    if (tab.protocol !== "ssh") return this.message("Server-Status gibt es nur bei SSH-Verbindungen – an der seriellen Konsole würde eine Abfrage mitten in deine Eingabe tippen.");
    if (tab.state !== "connected") return this.message("Nicht verbunden.");
    const cached = this.last.get(tab.id);
    if (cached) this.render(tab, cached);
    else this.message("Lese Status …");
    // Tab events can come in bursts; a device gets one query per 5 s at most.
    if (Date.now() - (this.polled.get(tab.id) || 0) >= POLL_MS) this.poll();
    else this.timer = setTimeout(() => this.poll(), POLL_MS);
  }

  async poll() {
    const id = this.tabId;
    if (!this.open || !id || this.busy) return;
    this.busy = true;
    this.polled.set(id, Date.now());
    try {
      const snap = await api.ServerStatus(id);
      this.last.set(id, snap);
      const tab = this.tabs.byId(id);
      if (tab && id === this.tabId) this.render(tab, snap);
    } catch (e) {
      if (id === this.tabId) this.message(errMsg(e), true);
    } finally {
      this.busy = false;
      if (this.open && id !== this.tabId) this.refresh(); // tab changed meanwhile: start polling the new one
      else if (this.open && this.tabs.byId(id)?.state === "connected") this.timer = setTimeout(() => this.poll(), POLL_MS);
    }
  }

  message(text, err = false) {
    this.el.replaceChildren(this.head(), h("div", { class: "sftp-msg" + (err ? " err" : "") }, text));
  }

  head(snap) {
    return h("div", { class: "st-head" },
      h("span", { class: "pane-title" }, "Server-Status"),
      snap ? h("span", { class: "st-time" }, new Date(snap.taken).toLocaleTimeString("de-DE")) : null,
      h("button", { class: "btn icon", title: "Schließen (Alt+I)", onclick: () => this.setOpen(false) }, "✕"));
  }

  render(tab, s) {
    const cisco = s.kind === "cisco";
    const parts = [this.head(s),
      h("div", { class: "st-host" }, h("b", {}, s.host || tab.title), h("div", { class: "muted" }, s.os || ""),
        h("div", { class: "muted" }, "Uptime: " + (s.uptime || fmtUptime(s.uptimeSec))))];
    if (cisco) {
      parts.push(bar("CPU (5 s)", Math.round(s.cpu[0] || 0), `1 min: ${s.cpu[1]} % · 5 min: ${s.cpu[2]} %`));
    } else {
      const load = s.load || [];
      const cores = `${s.cpus} Kern${s.cpus === 1 ? "" : "en"}`;
      const loadText = `Last ${load.map((x) => x.toFixed(2)).join(" · ")} bei ${cores}`;
      if (s.cpuUsage >= 0) parts.push(bar("CPU (letzte Sekunde)", Math.round(s.cpuUsage), loadText));
      else parts.push(bar("Last (1 min)", Math.min(s.cpus ? Math.round((load[0] / s.cpus) * 100) : 0, 100), loadText));
    }
    if (s.memTotal) parts.push(bar(cisco ? "Speicher (Processor Pool)" : "RAM", pct(s.memUsed, s.memTotal), `${fmtSize(s.memUsed)} von ${fmtSize(s.memTotal)}`));
    if (s.swapTotal) parts.push(bar("Swap", pct(s.swapUsed, s.swapTotal), `${fmtSize(s.swapUsed)} von ${fmtSize(s.swapTotal)}`));
    for (const d of s.disks || []) parts.push(bar(`Platte ${d.mount}`, pct(d.used, d.total), `${fmtSize(d.used)} von ${fmtSize(d.total)}`));
    if (s.procs?.length) {
      parts.push(h("div", { class: "st-procs" },
        h("div", { class: "st-proc hdr" }, h("span", { class: "n" }, "Prozess"),
          h("span", { title: cisco ? "" : `Anteil an der ganzen Maschine (alle ${s.cpus} Kerne) in der letzten Sekunde` }, "CPU"), cisco ? null : h("span", {}, "RAM")),
        s.procs.map((p) => h("div", { class: "st-proc" + (cisco ? " cisco" : ""), title: `PID ${p.pid}` },
          h("span", { class: "n" }, p.name), h("span", { title: cisco || !s.cpus ? "" : `≈ ${((p.cpu * s.cpus) / 100).toFixed(1)} von ${s.cpus} Kernen` }, p.cpu.toFixed(1) + " %"), cisco ? null : h("span", {}, p.mem.toFixed(1) + " %")))));
    }
    parts.push(h("div", { class: "hint st-foot" }, `Aktualisiert alle ${POLL_MS / 1000} s über einen eigenen SSH-Kanal – im Terminal tippt nichts mit.`));
    this.el.replaceChildren(...parts);
  }
}
