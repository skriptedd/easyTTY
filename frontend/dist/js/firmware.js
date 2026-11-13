// Firmware server: serves a local folder read-only via TFTP, HTTP and SCP/SFTP
// so a switch can pull its image with "copy … flash:". Ready-made commands are
// typed into the active terminal (without Enter – you check and confirm).
import { api, clipboard, errMsg, events } from "./api.js";
import { fmtSize, h, icon, modal, store, toast } from "./ui.js";
import { fwProgress } from "./fwprogress.js";

const CFG_KEY = "easytty.firmware";
const DEFAULTS = { dir: "", bindIp: "", tftp: true, tftpPort: 69, http: true, httpPort: 8080, ssh: true, sshPort: 22, uploads: false };
const LOG_SHOWN = 60;

let log = [];          // recent events (newest last)
let onEvent = null;    // set while the dialog is open
let onRunning = () => {};

/** Call once at startup; running(bool) updates the header button, currentTab() is the active tab. */
export function initFirmware(running, currentTab) {
  onRunning = running;
  events.on("fw:event", (e) => {
    log = [...log.slice(-199), e];
    if (e.proto === "Server") onRunning(e.detail.startsWith("gestartet"));
    if (onEvent) onEvent(e);
    fwProgress(e, currentTab); // progress card, also while the dialog is closed
  });
}

function field(label, input, cls = "") {
  return h("label", { class: "field " + cls }, h("span", {}, label), input);
}

/** Cisco IOS commands for one file. Returns [[label, command, hint]]. */
export function ciscoCommands(file, info, ip) {
  const c = info.config;
  const base = file.name.split("/").pop();
  const out = [["Test", `ping ${ip}`, ""]];
  // Fastest first: HTTP and TFTP are unencrypted; small switches decrypt SCP on a weak CPU.
  if (c.httpPort) out.push(["HTTP", `copy http://${ip}${c.httpPort === 80 ? "" : ":" + c.httpPort}/${file.name} flash:${base}`, ""]);
  if (c.tftpPort) out.push(["TFTP", `copy tftp://${ip}/${file.name} flash:${base}`, c.tftpPort !== 69 ? "IOS kennt bei TFTP nur Port 69" : ""]);
  if (c.sshPort) out.push(["SCP", `copy scp://${info.user}:${info.password}@${ip}/${file.name} flash:${base}`,
    "verschlüsselt – auf kleinen Switchen (z. B. 2960) oft nur 10–50 KB/s, HTTP ist viel schneller" + (c.sshPort !== 22 ? "; ältere IOS nutzen für SCP nur Port 22" : "")]);
  if (file.md5) out.push(["Prüfen", `verify /md5 flash:${base} ${file.md5}`, ""]);
  return out;
}

function eventLine(e) {
  const t = new Date(e.time).toLocaleTimeString("de-DE");
  const size = e.total ? ` · ${fmtSize(e.done)} / ${fmtSize(e.total)}` : "";
  return h("div", { class: "fw-ev " + e.state },
    h("span", { class: "t" }, t), h("b", {}, e.upload ? e.proto + " ⬆" : e.proto),
    h("span", {}, [e.client, e.file, e.state].filter(Boolean).join(" · ") + size + (e.detail ? ` – ${e.detail}` : "")));
}

/** Opens the firmware server dialog. tabs: TabManager (for "insert into terminal"). */
export async function openFirmware(tabs) {
  let state;
  try { state = await api.FirmwareState(); } catch (e) { toast(errMsg(e), "err"); return; }
  log = state.events || log;
  const cfg = { ...DEFAULTS, ...store.get(CFG_KEY, {}) };
  if (!cfg.dir) cfg.dir = state.defaultDir;
  let info = state.info;
  let files = [];
  let selected = null;

  const dir = h("input", { value: info.running ? info.config.dir : cfg.dir, spellcheck: false });
  const ipSel = h("select", {},
    h("option", { value: "" }, "alle Netzwerkkarten"),
    state.addresses.map((a) => h("option", { value: a.ip, selected: a.ip === cfg.bindIp }, `${a.ip} – ${a.interface}`)));
  const proto = (key, label) => {
    const on = h("input", { type: "checkbox", checked: cfg[key] });
    const port = h("input", { type: "number", min: 1, max: 65535, value: cfg[key + "Port"], class: "fw-port" });
    return { on, port, el: h("label", { class: "check" }, on, label, port) };
  };
  const tftp = proto("tftp", "TFTP"), http = proto("http", "HTTP"), ssh = proto("ssh", "SCP/SFTP");
  const uploads = h("input", { type: "checkbox", checked: cfg.uploads });
  const uploadDir = h("span", { class: "fw-updir" }, "…");
  api.ConfigDir().then((d) => { uploadDir.textContent = d; uploadDir.title = d; }).catch((e) => { uploadDir.textContent = errMsg(e); });
  const statusEl = h("div", { class: "fw-status" });
  const filesEl = h("div", { class: "fw-files" });
  const cmdsEl = h("div", { class: "fw-cmds" });
  const logEl = h("div", { class: "fw-log" });
  const fwEl = h("div", { class: "full hint fw-fw" }, "Windows-Firewall wird geprüft …");
  api.FirewallStatus().then((r) => {
    fwEl.textContent = (r.level === "ok" ? "✓ " : r.level === "warn" ? "⚠ " : "") + r.message;
    fwEl.className = "full hint fw-fw " + r.level;
  }).catch(() => { fwEl.textContent = "Firewall nicht prüfbar."; });
  const startBtn = h("button", { class: "btn primary" });
  const configEls = [dir, ipSel, tftp.on, tftp.port, http.on, http.port, ssh.on, ssh.port, uploads];

  const shownIp = () => (info.running ? info.config.bindIp : ipSel.value) || state.addresses[0]?.ip || "IP-DES-PCS";

  const renderStatus = () => {
    configEls.forEach((el) => { el.disabled = info.running; });
    if (info.running) {
      const ip = info.config.bindIp || "";
      if (![...ipSel.options].some((o) => o.value === ip)) ipSel.append(h("option", { value: ip }, ip));
      ipSel.value = ip;
    }
    startBtn.textContent = info.running ? "Stoppen" : "Starten";
    startBtn.classList.toggle("danger", info.running);
    const c = info.config;
    statusEl.replaceChildren(info.running
      ? h("div", { class: "fw-on" }, h("b", {}, "● läuft"),
        h("span", {}, [c.tftpPort && `TFTP :${c.tftpPort}`, c.httpPort && `HTTP :${c.httpPort}`, c.sshPort && `SCP/SFTP :${c.sshPort}`].filter(Boolean).join(" · ")),
        c.sshPort ? h("span", { class: "fp" }, `Login: ${info.user} / ${info.password}`) : null,
        c.uploadDir ? h("span", {}, "· nimmt Configs an") : null)
      : h("div", { class: "hint" }, "Gestoppt. Geräte können Dateien nur holen, löschen geht nie. Hochladen nur, wenn „Configs annehmen“ an ist, und dann nur in den Config-Ordner. Windows fragt beim ersten Start evtl. nach der Firewall-Freigabe."));
    renderCmds();
  };

  const renderFiles = () => {
    if (!files.length) {
      filesEl.replaceChildren(h("div", { class: "sftp-msg" }, "Keine Dateien im Ordner. Leg das Image hinein (Ordner öffnen) und klick auf „Neu laden“ rechts neben dem Ordner."));
      return;
    }
    filesEl.replaceChildren(...files.map((f) => h("div", {
      class: "fw-file" + (selected === f ? " sel" : ""),
      onclick: () => { selected = f; renderFiles(); renderCmds(); },
    }, h("span", { class: "n", title: f.name }, f.name), h("span", {}, fmtSize(f.size)), h("span", { class: "md5", title: "MD5" }, f.md5 || "…"))));
  };

  const renderCmds = () => {
    const backup = info.running && info.config.uploadDir
      ? [["Config sichern", `copy running-config tftp://${shownIp()}/`, "Den Dateinamen fragt der Switch nach (Vorschlag: Hostname-confg). Jede Sicherung bekommt eine eigene Datei."]]
      : [];
    if (!selected && !backup.length) return cmdsEl.replaceChildren(h("div", { class: "hint" }, "Datei auswählen – dann stehen hier die fertigen Befehle für den Switch."));
    if (!info.running) return cmdsEl.replaceChildren(h("div", { class: "hint" }, "Server starten, dann stehen hier die Befehle für " + selected.name + "."));
    const tab = tabs.current();
    const canInsert = tab && tab.state === "connected";
    cmdsEl.replaceChildren(...[...(selected ? ciscoCommands(selected, info, shownIp()) : []), ...backup].map(([label, cmd, hint]) => h("div", { class: "fw-cmd" },
      h("b", { title: hint }, label, hint ? h("span", { class: "fw-warn" }, " ⚠") : null),
      h("div", {}, h("code", {}, cmd), hint ? h("div", { class: "hint" }, hint) : null),
      h("button", { class: "btn", title: "In die Zwischenablage", onclick: () => { clipboard.set(cmd); toast("Kopiert"); } }, "Kopieren"),
      h("button", {
        class: "btn", disabled: !canInsert,
        title: canInsert ? `In „${tab.title}“ tippen (ohne Enter)` : "Kein verbundener Tab",
        onclick: () => insert(cmd),
      }, "Einfügen"))),
    h("div", { class: "hint" }, `Adresse ${shownIp()} – ` + (canInsert ? `„Einfügen“ tippt in „${tab.title}“, Enter drückst du selbst.` : "öffne zuerst die Switch-Sitzung zum Einfügen.")),
    h("div", { class: "hint" }, "Taucht nach dem copy nichts im Protokoll auf, erreicht der Switch den PC nicht. Dann erst „ping“ testen: Der Switch braucht eine IP im selben Netz (z. B. interface vlan 1 + ip address) oder ein Gateway (ip default-gateway). Im Browser zeigt http://IP:Port/ die Dateiliste."));
  };

  const renderLog = () => {
    const items = log.slice(-LOG_SHOWN).reverse();
    logEl.replaceChildren(...(items.length ? items.map(eventLine) : [h("div", { class: "hint" }, "Noch keine Zugriffe.")]));
  };

  const loadFiles = async () => {
    try {
      files = await api.FirmwareFiles(dir.value.trim());
      selected = files.find((f) => f.name === selected?.name) || (files.length === 1 ? files[0] : null);
    } catch (e) { files = []; toast(errMsg(e), "err"); }
    renderFiles();
    renderCmds();
  };

  const toggle = async () => {
    startBtn.disabled = true;
    try {
      if (info.running) {
        await api.FirmwareStop();
        info = { ...info, running: false };
      } else {
        const next = { dir: dir.value.trim(), bindIp: ipSel.value,
          tftp: tftp.on.checked, tftpPort: +tftp.port.value || 69,
          http: http.on.checked, httpPort: +http.port.value || 8080,
          ssh: ssh.on.checked, sshPort: +ssh.port.value || 22, uploads: uploads.checked };
        store.set(CFG_KEY, next);
        const uploadDirPath = next.uploads ? await api.ConfigDir() : "";
        info = await api.FirmwareStart({ dir: next.dir, bindIp: next.bindIp, uploadDir: uploadDirPath,
          tftpPort: next.tftp ? next.tftpPort : 0, httpPort: next.http ? next.httpPort : 0, sshPort: next.ssh ? next.sshPort : 0 });
        dir.value = info.config.dir;
        await loadFiles();
      }
      onRunning(info.running);
    } catch (e) { toast(errMsg(e), "err", 9000); }
    startBtn.disabled = false;
    renderStatus();
  };

  let close;
  const insert = (cmd) => {
    const tab = tabs.current();
    if (!tab || !tab.term) { toast("Befehle gehen nur in SSH- oder Serial-Tabs.", "err"); return; }
    api.Write(tab.id, cmd).then(() => { close(true); tab.focus(); }).catch((e) => toast(errMsg(e), "err"));
  };

  startBtn.addEventListener("click", toggle);
  dir.addEventListener("change", loadFiles);
  ipSel.addEventListener("change", renderCmds);
  onEvent = (e) => {
    renderLog();
    if (e.proto === "Server" && !e.detail.startsWith("gestartet") && info.running) { info = { ...info, running: false }; renderStatus(); }
  };
  renderStatus();
  renderLog();
  loadFiles();

  await modal((c) => {
    close = c;
    return {
      head: "Firmware-Server",
      wide: true,
      body: h("div", { class: "grid fw" },
        field("Ordner mit den Images", h("div", { class: "row" }, dir,
          h("button", { class: "btn", title: "Ordner wählen", onclick: async () => {
            const p = await api.PickFirmwareDir(dir.value).catch(() => "");
            if (p) { dir.value = p; loadFiles(); }
          } }, "…"),
          h("button", { class: "btn", title: "Im Explorer öffnen", onclick: () => api.OpenFirmwareDir(dir.value.trim()).catch((e) => toast(errMsg(e), "err")) }, "Öffnen"),
          h("button", { class: "btn", title: "Liste neu laden", onclick: loadFiles }, icon("refresh"))), "full"),
        field("Erreichbar über", ipSel),
        field("Protokolle & Ports", h("div", { class: "fw-protos" }, tftp.el, http.el, ssh.el)),
        field("Running-Configs empfangen (copy running-config tftp:)", h("div", { class: "row" },
          h("label", { class: "check", title: "Geräte dürfen per TFTP Dateien in den Config-Ordner schreiben. Nichts wird überschrieben." }, uploads, "Configs annehmen in"),
          uploadDir,
          h("button", { class: "btn", title: "Config-Ordner wählen", onclick: async () => {
            const p = await api.PickConfigDir().catch((e) => { toast(errMsg(e), "err"); return ""; });
            if (p) { uploadDir.textContent = p; uploadDir.title = p; }
          } }, "…"),
          h("button", { class: "btn", title: "Im Explorer öffnen", onclick: () => api.OpenConfigDir().catch((e) => toast(errMsg(e), "err")) }, "Öffnen")), "full"),
        h("div", { class: "full fw-bar" }, statusEl, startBtn),
        fwEl,
        field("Dateien (Name · Größe · MD5)", filesEl, "full"),
        field("Befehle für das Gerät", cmdsEl, "full"),
        field("Protokoll", logEl, "full")),
      foot: [h("button", { class: "btn", onclick: () => c(false) }, "Schließen")],
    };
  });
  onEvent = null;
}
