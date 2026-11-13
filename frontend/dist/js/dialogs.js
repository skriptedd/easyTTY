// Session editor, auth/host-key prompts and settings dialog.
import { api, errMsg } from "./api.js";
import { h, icon, modal, toast } from "./ui.js";
import { fontPicker } from "./fonts.js";
import { highlightFields } from "./highlightEditor.js";
import { mcpFields } from "./mcp.js";

export const COLORS = ["", "#ff5d6c", "#f2b84b", "#3ccf91", "#4fb3ff", "#a68bff", "#ff8ad8", "#8a94a6"];
const BAUDS = [1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400];

/** "COM6 – USB Serial Port · FTDI ★" (★ = typical console cable). */
export function portLabel(p) {
  const parts = [p.description, p.vendor].filter(Boolean).join(" · ");
  return `${p.name}${parts ? " – " + parts : ""}${p.console ? "  ★ Konsolenkabel" : ""}`;
}

/** Prefer a console cable (FTDI, Prolific, CH340, Cisco …) over other serial devices. */
function defaultPort(ports) {
  return (ports.find((p) => p.console) || ports[0] || { name: "" }).name;
}

function field(label, input, cls = "") {
  return h("label", { class: "field " + cls }, h("span", {}, label), input);
}

/** Flattened folder list with indentation for <select>. */
export function folderOptions(folders, selected) {
  const byParent = new Map();
  for (const f of folders) {
    if (!byParent.has(f.parentId)) byParent.set(f.parentId, []);
    byParent.get(f.parentId).push(f);
  }
  const out = [h("option", { value: "", selected: selected === "" }, "(Oberste Ebene)")];
  const walk = (parent, depth) => {
    const kids = (byParent.get(parent) || []).sort((a, b) => a.name.localeCompare(b.name));
    for (const f of kids) {
      out.push(h("option", { value: f.id, selected: f.id === selected }, "\u00A0\u00A0".repeat(depth + 1) + f.name));
      walk(f.id, depth + 1);
    }
  };
  walk("", 0);
  return out;
}

/**
 * Opens the session editor. Resolves with {session, connect} or null.
 * sessions: all saved sessions (to pick a jump host from).
 */
export async function editSession(initial, folders, sessions = []) {
  const s = {
    protocol: "ssh", port: 22, auth: "auto", baud: 9600, dataBits: 8, parity: "none", stopBits: "1",
    keepAlive: 30, color: "", folderId: "", ...initial,
  };
  const ports = await api.ListSerialPorts().catch(() => []);

  const name = h("input", { value: s.name || "", placeholder: "z. B. core-sw1", autofocus: true });
  const nameHint = h("div", { class: "hint" });
  let nameTimer = 0;
  const showNameHint = () => {
    clearTimeout(nameTimer);
    nameTimer = setTimeout(async () => {
      const typed = name.value.trim();
      const formatted = typed ? await api.FormatSessionName(typed).catch(() => typed) : "";
      nameHint.textContent = formatted && formatted !== typed ? `Wird nach Namensschema gespeichert als: ${formatted}` : "";
    }, 150);
  };
  name.addEventListener("input", showNameHint);
  showNameHint();
  const folder = h("select", {}, folderOptions(folders, s.folderId || ""));
  const host = h("input", { value: s.host || "", placeholder: "10.0.0.1 oder host.example", spellcheck: "false" });
  const port = h("input", { value: s.port || 22, type: "number", min: 1, max: 65535 });
  const user = h("input", { value: s.protocol === "ssh" || !s.protocol ? s.user || "" : "", placeholder: "admin · Konsolenserver: admin:6", spellcheck: "false",
    title: "Konsolenserver: Benutzer:Port, z. B. admin:6 für das Gerät an Port 6" });
  const auth = h("select", {},
    h("option", { value: "auto" }, "Automatisch (Agent, Schlüssel, Passwort)"),
    h("option", { value: "password" }, "Passwort"),
    h("option", { value: "key" }, "Schlüsseldatei"),
    h("option", { value: "agent" }, "SSH-Agent"),
  );
  auth.value = s.auth;
  const keyPath = h("input", { value: s.keyPath || "", placeholder: "~/.ssh/id_ed25519", spellcheck: "false" });
  const keyBtn = h("button", { class: "btn", type: "button", onclick: async () => {
    const p = await api.PickKeyFile().catch(() => "");
    if (p) keyPath.value = p;
  } }, "…");
  const password = h("input", { type: "password", placeholder: s.hasPassword ? "gespeichert – leer lassen zum Behalten" : "optional" });
  const savePw = h("input", { type: "checkbox", checked: true });
  password.addEventListener("input", () => { if (password.value) savePw.checked = true; });
  const clearPw = h("input", { type: "checkbox" });
  const keepAlive = h("input", { value: s.keepAlive, type: "number", min: 0 });
  const jumps = sessions.filter((x) => (x.protocol || "ssh") === "ssh" && x.id !== s.id)
    .sort((a, b) => a.name.localeCompare(b.name, "de"));
  const jump = h("select", {}, h("option", { value: "" }, "Direkt verbinden"),
    jumps.map((x) => h("option", { value: x.id }, `${x.name} (${x.host})`)));
  jump.value = s.jumpId || "";
  const forwards = h("textarea", { rows: 2, spellcheck: "false", placeholder: "8443 10.0.0.1:443   (ein Eintrag pro Zeile: lokaler Port, Ziel)" },
    (s.forwards || []).map((f) => `${f.listen} ${f.host.includes(":") ? `[${f.host}]` : f.host}:${f.port}`).join("\n"));

  // RDP (opens Windows' Remote Desktop): own inputs, the SSH ones stay untouched.
  const isRdp = s.protocol === "rdp";
  const rdpHost = h("input", { value: isRdp ? s.host || "" : "", placeholder: "Servername oder IP", spellcheck: "false" });
  const rdpPort = h("input", { value: isRdp ? s.port || 3389 : 3389, type: "number", min: 1, max: 65535 });
  const rdpUser = h("input", { value: isRdp ? s.user || "" : "", placeholder: "DOMÄNE\\benutzer oder benutzer@domäne", spellcheck: "false" });
  const rdpPw = h("input", { type: "password", placeholder: isRdp && s.hasPassword ? "gespeichert – leer lassen zum Behalten" : "optional – sonst fragt Windows" });
  const rdpClearPw = h("input", { type: "checkbox" });
  const rdpDrives = h("input", { type: "checkbox", checked: !!s.rdpDrives });
  const rdpClip = h("input", { type: "checkbox", checked: !s.rdpNoClipboard });

  const serialPort = h("select", {});
  const portHint = h("div", { class: "hint" });
  let portInfos = ports;
  const fillPorts = (selected) => {
    const opts = portInfos.map((p) => h("option", { value: p.name }, portLabel(p)));
    if (selected && !portInfos.some((p) => p.name === selected)) {
      opts.push(h("option", { value: selected }, `${selected} – nicht angeschlossen`));
    }
    if (!opts.length) opts.push(h("option", { value: "" }, "Kein COM-Port gefunden – Kabel einstecken und neu einlesen"));
    serialPort.replaceChildren(...opts);
    serialPort.value = selected || defaultPort(portInfos);
    showPortHint();
  };
  const showPortHint = () => {
    const p = portInfos.find((x) => x.name === serialPort.value);
    portHint.textContent = !p ? "" : p.console
      ? "Konsolenkabel erkannt."
      : "Achtung: sieht nicht nach einem Konsolenkabel aus" + (p.vendor ? ` (${p.vendor})` : "") + ".";
    portHint.style.color = p && !p.console ? "var(--warn)" : "";
  };
  serialPort.addEventListener("change", showPortHint);
  const refreshPorts = h("button", { class: "btn", type: "button", title: "Ports neu einlesen", onclick: async () => {
    portInfos = await api.ListSerialPorts().catch(() => portInfos);
    fillPorts(serialPort.value);
  } }, icon("refresh"));
  fillPorts(s.serialPort || "");
  const baud = h("input", { value: s.baud, list: "bauds", type: "number" });
  const baudList = h("datalist", { id: "bauds" }, BAUDS.map((b) => h("option", { value: b })));
  const dataBits = h("select", {}, [8, 7, 6, 5].map((b) => h("option", { value: b }, b)));
  dataBits.value = String(s.dataBits);
  const parity = h("select", {}, [["none", "Keine"], ["even", "Gerade"], ["odd", "Ungerade"], ["mark", "Mark"], ["space", "Space"]]
    .map(([v, l]) => h("option", { value: v }, l)));
  parity.value = s.parity;
  const stopBits = h("select", {}, ["1", "1.5", "2"].map((v) => h("option", { value: v }, v)));
  stopBits.value = s.stopBits;

  const logPath = h("input", { value: s.logPath || "", placeholder: "optional, z. B. C:\\Logs\\{session}_{date}.log", spellcheck: "false" });
  const logBtn = h("button", { class: "btn", type: "button", onclick: async () => {
    const p = await api.PickLogFile().catch(() => "");
    if (p) logPath.value = p;
  } }, "…");
  const notes = h("textarea", { spellcheck: "false" }, s.notes || "");
  const logRow = field("Eigener Log-Pfad (optional – sonst automatisches Logging aus den Einstellungen)", h("div", { class: "row" }, logPath, logBtn), "full");

  let color = s.color || "";
  const swatches = h("div", { class: "swatches" }, COLORS.map((c) => h("span", {
    class: "swatch" + (c ? "" : " none") + (c === color ? " on" : ""),
    style: c ? { background: c } : {},
    title: c || "keine Farbe",
    onclick: (e) => {
      color = c;
      swatches.querySelectorAll(".swatch").forEach((x) => x.classList.remove("on"));
      e.currentTarget.classList.add("on");
    },
  })));

  let protocol = s.protocol;
  const sshBox = h("div", { class: "grid full" },
    field("Host", host), field("Port", port),
    field("Benutzer", user), field("Anmeldung", auth),
    field("Schlüsseldatei", h("div", { class: "row" }, keyPath, keyBtn), "full key-row"),
    field("Passwort", password),
    h("div", { class: "field" }, h("span", {}, "\u00A0"),
      s.hasPassword
        ? h("label", { class: "check" }, clearPw, "Gespeichertes Passwort entfernen")
        : h("label", { class: "check" }, savePw, "Passwort verschlüsselt speichern")),
    field("Keepalive (Sekunden, 0 = aus)", keepAlive),
    field("Sprung-Host (wie ssh -J)", jump),
    field("Port-Weiterleitungen (wie ssh -L)", forwards, "full"),
    h("div", { class: "full hint" }, "Über den Sprung-Host geht die Verbindung durch eine andere gespeicherte SSH-Session, mit deren Anmeldung. Weiterleitungen öffnen 127.0.0.1:Port auf diesem PC, solange die Session verbunden ist."),
  );
  // Console login (optional): answered automatically right after connecting.
  const serialUser = h("input", { value: s.protocol === "serial" ? s.user || "" : "", placeholder: "z. B. admin", spellcheck: "false" });
  const serialPw = h("input", { type: "password", placeholder: s.protocol === "serial" && s.hasPassword ? "gespeichert – leer lassen zum Behalten" : "optional" });
  const serialClearPw = h("input", { type: "checkbox" });
  const serialBox = h("div", { class: "grid full" },
    h("div", { class: "field full" }, h("span", {}, "COM-Port"), h("div", { class: "row" }, serialPort, refreshPorts), portHint),
    field("Baudrate", baud),
    field("Datenbits", dataBits), field("Parität", parity),
    field("Stoppbits", stopBits), baudList,
    h("div", { class: "field full section" }, h("span", {}, "Anmeldung an der Konsole (optional)")),
    field("Benutzer", serialUser), field("Passwort", serialPw),
    s.protocol === "serial" && s.hasPassword ? h("label", { class: "check full" }, serialClearPw, "Gespeichertes Passwort entfernen") : null,
    h("div", { class: "full hint" }, "Nach dem Verbinden tippt easyTTY bei „Username:“/„login:“ den Benutzer und beim folgenden „Password:“ das Passwort (verschlüsselt gespeichert). Nur in den ersten 30 Sekunden – ein späteres „Password:“ (z. B. nach enable) bekommt es nie. Nach einem Timeout: Tab-Rechtsklick → „Anmelden“."),
  );
  const rdpBox = h("div", { class: "grid full" },
    field("Host", rdpHost), field("Port", rdpPort),
    field("Benutzer", rdpUser), field("Passwort", rdpPw),
    isRdp && s.hasPassword ? h("label", { class: "check full" }, rdpClearPw, "Gespeichertes Passwort entfernen") : null,
    h("label", { class: "check full" }, rdpClip, "Zwischenablage mit dem Remotedesktop teilen (Kopieren und Einfügen in beide Richtungen)"),
    h("label", { class: "check full" }, rdpDrives, "Lokale Laufwerke im Remotedesktop zeigen"),
    h("div", { class: "full hint" }, "Öffnet als Tab in easyTTY, die Auflösung folgt der Tab-Größe. Das Passwort liegt verschlüsselt in easyTTY und geht direkt an das Remotedesktop-Steuerelement von Windows. Rechtsklick auf die Session → „In eigenem Remotedesktop-Fenster öffnen“ für Vollbild."),
  );
  const seg = h("div", { class: "seg" },
    h("button", { type: "button", "data-p": "ssh" }, "SSH"),
    h("button", { type: "button", "data-p": "serial" }, "Serial"),
    h("button", { type: "button", "data-p": "rdp" }, "RDP"),
  );
  const sync = () => {
    seg.querySelectorAll("button").forEach((b) => b.classList.toggle("on", b.dataset.p === protocol));
    sshBox.style.display = protocol === "ssh" ? "" : "none";
    serialBox.style.display = protocol === "serial" ? "" : "none";
    rdpBox.style.display = protocol === "rdp" ? "" : "none";
    logRow.style.display = protocol === "rdp" ? "none" : "";
    sshBox.querySelector(".key-row").style.display = auth.value === "key" ? "" : "none";
  };
  seg.addEventListener("click", (e) => { if (e.target.dataset.p) { protocol = e.target.dataset.p; sync(); } });
  auth.addEventListener("change", sync);
  sync();

  const collect = () => ({
    ...initial,
    id: s.id || "",
    name: name.value.trim(),
    folderId: folder.value,
    protocol,
    host: (protocol === "rdp" ? rdpHost : host).value.trim(),
    port: protocol === "rdp" ? parseInt(rdpPort.value, 10) || 3389 : parseInt(port.value, 10) || 22,
    user: (protocol === "serial" ? serialUser : protocol === "rdp" ? rdpUser : user).value.trim(),
    rdpDrives: rdpDrives.checked,
    rdpNoClipboard: !rdpClip.checked,
    auth: auth.value,
    keyPath: keyPath.value.trim(),
    keepAlive: Math.max(0, parseInt(keepAlive.value, 10) || 0),
    jumpId: protocol === "ssh" ? jump.value : "",
    forwards: protocol === "ssh" ? parseForwards(forwards.value) : [],
    serialPort: serialPort.value.trim(),
    baud: parseInt(baud.value, 10) || 9600,
    dataBits: parseInt(dataBits.value, 10),
    parity: parity.value,
    stopBits: stopBits.value,
    logPath: logPath.value.trim(),
    notes: notes.value,
    color,
  });

  const pwArg = () => {
    if (protocol === "serial") {
      if (s.hasPassword && serialClearPw.checked) return "";
      return serialPw.value ? serialPw.value : null;
    }
    if (protocol === "rdp") {
      if (isRdp && s.hasPassword && rdpClearPw.checked) return "";
      return rdpPw.value ? rdpPw.value : null;
    }
    if (protocol !== "ssh") return null;
    if (s.hasPassword && clearPw.checked) return "";
    if (password.value && (s.hasPassword || savePw.checked)) return password.value;
    return null;
  };

  return modal((close) => {
    const save = async (connect) => {
      try {
        const saved = await api.SaveSession(collect(), pwArg());
        close({ session: saved, connect });
      } catch (e) {
        toast(errMsg(e), "err");
      }
    };
    return {
      head: s.id ? "Session bearbeiten" : "Neue Session",
      wide: true,
      sticky: true,
      body: h("div", { class: "grid" },
        h("label", { class: "field" }, h("span", {}, "Name"), name, nameHint), field("Ordner", folder),
        h("div", { class: "field full" }, h("span", {}, "Protokoll"), seg),
        sshBox, serialBox, rdpBox,
        logRow,
        h("div", { class: "field full" }, h("span", {}, "Farbe"), swatches),
        field("Notizen", notes, "full"),
      ),
      foot: [
        h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
        h("button", { class: "btn", onclick: () => save(false) }, "Speichern"),
        h("button", { class: "btn primary", onclick: () => save(true) }, "Speichern & verbinden"),
      ],
      onKey: (e) => { if (e.key === "Enter" && e.ctrlKey) { e.preventDefault(); save(true); } },
    };
  }).then((v) => v || null);
}

/* ---------- backend prompts (host key, password, passphrase, username) ---------- */

const openPrompts = new Map(); // id -> { close, ev }

const sameHostKey = (a, b) => a.kind === "hostkey" && b.kind === "hostkey" &&
  a.hostKey.host === b.hostKey.host && a.hostKey.fingerprint === b.hostKey.fingerprint;

export function handlePrompt(ev, tabTitle) {
  const inputs = (ev.fields || []).map((f, i) => h("input", {
    type: f.secret ? "password" : "text",
    value: f.value || "",
    autofocus: i === 0,
    spellcheck: "false",
  }));
  const isHostKey = ev.kind === "hostkey";
  const openedAt = Date.now();
  const p = modal((close) => {
    openPrompts.set(ev.id, { close, ev });
    const answer = (ok) => close({ ok, values: inputs.map((i) => i.value) });
    const body = [];
    if (tabTitle) body.push(h("p", { class: "hint" }, "Tab: " + tabTitle));
    if (ev.message) body.push(h("p", { style: { whiteSpace: "pre-line" } }, ev.message));
    if (isHostKey) {
      // Focus sits on the fingerprint (not on "Vertrauen"): Enter then goes through onKey and its delay.
      body.push(h("div", { class: "fp", tabindex: "-1", autofocus: true },
        h("div", {}, h("b", {}, "Host: "), ev.hostKey.host),
        h("div", {}, h("b", {}, "Typ: "), ev.hostKey.keyType),
        h("div", {}, h("b", {}, "Fingerprint: "), ev.hostKey.fingerprint)));
      const k = ev.hostKey;
      if (k.changed && k.oldFingerprint) {
        body.push(h("div", { class: "fp old" },
          h("div", {}, h("b", {}, "Bisher gespeichert: "), `${k.oldKeyType} ${k.oldFingerprint}`),
          h("div", {}, h("b", {}, "Aus: "), k.oldSource)));
        body.push(h("p", { class: "hint" }, "Neu aufgesetzt, Server ersetzt oder hat die IP gewechselt (DHCP)? Dann ist das normal: „Neuen Schlüssel speichern“ ersetzt den alten, danach fragt easyTTY nicht mehr."));
      }
    }
    ev.fields?.forEach((f, i) => body.push(field(f.label || "Eingabe", inputs[i])));
    return {
      head: ev.title || "Eingabe",
      danger: isHostKey && ev.hostKey.changed,
      sticky: true,
      body: h("div", { class: "grid" }, h("div", { class: "full" }, body)),
      foot: [
        h("button", { class: "btn", onclick: () => answer(false) }, "Abbrechen"),
        h("button", { class: "btn primary" + (isHostKey && ev.hostKey.changed ? " danger" : ""), onclick: () => answer(true) },
          isHostKey ? (ev.hostKey.changed ? "Neuen Schlüssel speichern" : "Vertrauen & verbinden") : "OK"),
      ],
      onKey: (e) => {
        // An Enter still meant for the terminal must not trust a host key unread.
        if (e.key !== "Enter" || (isHostKey && ev.hostKey.changed)) return;
        if (Date.now() - openedAt < 800 || !e.target.closest?.(".modal")) { e.preventDefault(); return; }
        if (e.target.tagName === "BUTTON") return; // the focused button handles its own Enter
        e.preventDefault();
        answer(true);
      },
    };
  });
  p.then((res) => {
    openPrompts.delete(ev.id);
    api.AnswerPrompt(ev.id, !!res?.ok, res?.values || []).catch((e) => toast(errMsg(e), "err"));
    // Several tiles to the same host ask in parallel: one "trust" answers all
    // prompts for exactly this host and fingerprint.
    if (res?.ok && isHostKey) {
      for (const other of [...openPrompts.values()]) {
        if (sameHostKey(other.ev, ev)) other.close({ ok: true, values: [] });
      }
    }
  });
}

export function cancelPrompt(id) {
  openPrompts.get(id)?.close(undefined);
}

/* ---------- logging ---------- */

/** Logging controls shared by the first-run and settings dialogs. */
function logFields(s) {
  const enabled = h("input", { type: "checkbox", checked: s.logEnabled });
  const dir = h("input", { value: s.logDir || "", spellcheck: "false" });
  const pick = h("button", { class: "btn", type: "button", onclick: async () => {
    const p = await api.PickLogDir(dir.value).catch(() => "");
    if (p) dir.value = p;
  } }, "…");
  const plain = h("input", { type: "checkbox", checked: !s.logRaw });
  const open = h("button", { class: "btn", type: "button", onclick: () => api.OpenLogDir().catch((e) => toast(errMsg(e), "err")) }, "Öffnen");
  const sync = () => { [dir, pick, plain].forEach((el) => { el.disabled = !enabled.checked; }); };
  enabled.addEventListener("change", sync);
  sync();
  return {
    elements: [
      h("label", { class: "check full" }, enabled, "Alle Sitzungen automatisch protokollieren"),
      field("Log-Ordner (pro Tag ein Unterordner, pro Sitzung eine Datei)", h("div", { class: "row" }, dir, pick, open), "full"),
      h("label", { class: "check full" }, plain, "Klartext mit Zeitstempel pro Zeile (Farbcodes entfernt) – empfohlen"),
    ],
    collect: () => ({ logEnabled: enabled.checked, logDir: dir.value.trim(), logRaw: !plain.checked }),
  };
}

/** First start: set up automatic session logging. Always resolves with saved settings. */
export async function firstRunSetup(current) {
  const log = logFields(current);
  const choice = await modal((close) => ({
    head: "Willkommen bei easyTTY",
    sticky: true,
    body: h("div", { class: "grid" },
      h("p", { class: "full" },
        "easyTTY protokolliert jede Sitzung automatisch in eine Textdatei – mit Datum und Uhrzeit vor jeder Zeile. ",
        "So kannst du später nachschlagen, was du wann auf welchem Gerät gemacht hast."),
      log.elements,
      h("p", { class: "full hint" },
        "Passwörter landen nicht im Log, da sie das Gerät nicht zurückschickt. Ändern kannst du das jederzeit in den Einstellungen (⚙)."),
    ),
    foot: [h("button", { class: "btn primary", onclick: () => close(log.collect()) }, "Los geht's")],
    onKey: (e) => { if (e.key === "Enter") { e.preventDefault(); close(log.collect()); } },
  }));
  try {
    return await api.SaveSettings({ ...current, ...(choice || {}), firstRunDone: true });
  } catch (e) {
    toast("Einstellungen speichern: " + errMsg(e), "err");
    return current;
  }
}

/* ---------- settings ---------- */

const NAME_CASES = [["", "Wie eingegeben"], ["lower", "alles klein (test switch)"], ["capitalize", "Erster Buchstabe groß (Test switch)"], ["title", "Jedes Wort groß (Test Switch)"], ["upper", "ALLES GROSS (TEST SWITCH)"]];
const NAME_SPACES = [["", "Leerzeichen behalten"], ["_", "Leerzeichen → _"], ["-", "Leerzeichen → -"]];
// One-click presets: [label, case, spaces].
const NAME_PRESETS = [["Test_Switch", "title", "_"], ["test_switch", "lower", "_"], ["Test-Switch", "title", "-"], ["test-switch", "lower", "-"], ["TEST_SWITCH", "upper", "_"], ["Test Switch", "title", ""]];

/** Mirror of store.Settings.FormatName for the live preview (backend is authoritative). */
function formatName(name, nameCase, spaces) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const up1 = (w) => w.charAt(0).toUpperCase() + w.slice(1);
  let out = words.join(" ");
  if (nameCase === "lower") out = out.toLowerCase();
  else if (nameCase === "upper") out = out.toUpperCase();
  else if (nameCase === "capitalize") out = up1(out.toLowerCase());
  else if (nameCase === "title") out = out.toLowerCase().replace(/(^|[ _-])(\S)/gu, (_, sep, c) => sep + c.toUpperCase());
  return spaces ? out.replace(/ /g, spaces) : out;
}

export async function editSettings(current, onImport, onRenamed, share = {}) {
  const s = { ...current };
  const nameCase = h("select", {}, NAME_CASES.map(([v, l]) => h("option", { value: v }, l)));
  nameCase.value = s.nameCase || "";
  const nameSpaces = h("select", {}, NAME_SPACES.map(([v, l]) => h("option", { value: v }, l)));
  nameSpaces.value = s.nameSpaces || "";
  const namePreview = h("div", { class: "hint" });
  const namePresets = h("div", { class: "name-presets" }, NAME_PRESETS.map(([label, c, sp]) => h("button", {
    class: "btn", type: "button", title: `Schreibweise ${label} übernehmen`,
    onclick: () => { nameCase.value = c; nameSpaces.value = sp; showPreview(); },
  }, label)));
  const showPreview = () => {
    namePreview.textContent = `Beispiel: „test switch“ → „${formatName("test switch", nameCase.value, nameSpaces.value)}“ · „core sw rz nord“ → „${formatName("core sw rz nord", nameCase.value, nameSpaces.value)}“`;
    for (const b of namePresets.children) {
      const [, c, sp] = NAME_PRESETS.find(([l]) => l === b.textContent);
      b.classList.toggle("on", c === nameCase.value && sp === nameSpaces.value);
    }
  };
  [nameCase, nameSpaces].forEach((el) => el.addEventListener("change", showPreview));
  showPreview();
  const font = fontPicker(s.fontFamily);
  const size = h("input", { value: s.fontSize, type: "number", min: 8, max: 32 });
  const theme = h("select", {}, h("option", { value: "dark" }, "Dunkel"), h("option", { value: "light" }, "Hell"));
  theme.value = s.theme;
  const scroll = h("input", { value: s.scrollback, type: "number", min: 0, max: 1000000 });
  const pasteDelay = h("input", { value: s.pasteLineDelay || 0, type: "number", min: 0, max: 2000 });
  const chk = (key, label) => {
    const c = h("input", { type: "checkbox", checked: s[key] });
    c.addEventListener("change", () => { s[key] = c.checked; });
    return h("label", { class: "check full" }, c, label);
  };
  const info = await api.AppInfo().catch(() => ({}));
  const log = logFields(s);
  const ai = mcpFields(s);
  const hl = highlightFields(s);
  return modal((close) => ({
    head: "Einstellungen",
    sticky: true,
    body: withSectionNav("Darstellung", h("div", { class: "grid" },
      field("Schriftart", font.el, "full"),
      field("Schriftgröße", size), field("Design", theme),
      field("Scrollback (Zeilen)", scroll, "full"),
      chk("copyOnSelect", "Markieren kopiert automatisch"),
      chk("rightClickPaste", "Rechtsklick fügt ein"),
      chk("cursorBlink", "Blinkender Cursor"),
      chk("confirmClose", "Beim Beenden mit offenen Verbindungen nachfragen"),
      chk("altShortcuts", "Kürzel mit Alt (Alt+W, Alt+B …). Aus: Alt geht ans Terminal, z. B. für Wortsprünge in bash, die Kürzel liegen dann auf Strg+Shift+Buchstabe"),
      h("div", { class: "field full section" }, h("span", {}, "Einfügen (z. B. Running-Config)")),
      chk("pasteConfirm", "Vor dem Einfügen mehrerer Zeilen den Text zum Prüfen und Bearbeiten zeigen"),
      field("Zusätzliche Pause pro Zeile (ms) – nur nötig bei sehr langsamen Geräten", pasteDelay, "full"),
      h("div", { class: "field full section" }, h("span", {}, "Serial-Konsole")),
      chk("serialWake", "Nach dem Öffnen eines COM-Ports automatisch Enter senden (Prompt erscheint sofort)"),
      h("div", { class: "field full section" }, h("span", {}, "Session-Namen")),
      field("Schnellwahl", namePresets, "full"),
      field("Groß-/Kleinschreibung", nameCase), field("Leerzeichen", nameSpaces),
      h("div", { class: "full" }, namePreview,
        h("div", { class: "hint" }, "Gilt automatisch bei jedem Speichern einer Session (auch Quick Connect, Duplizieren, Import)."),
        h("button", { class: "btn", style: { marginTop: "6px" }, onclick: async () => {
          try {
            await api.SaveSettings({ ...(await api.GetSettings()), mcpToken: "", nameCase: nameCase.value, nameSpaces: nameSpaces.value });
            const n = await api.ApplyNamingToAll();
            toast(n ? `${n} Session(s) umbenannt.` : "Alle Namen passen schon zum Schema.", "ok");
            onRenamed?.();
          } catch (e) { toast(errMsg(e), "err"); }
        } }, "Schema jetzt auf alle bestehenden Sessions anwenden")),
      h("div", { class: "field full section" }, h("span", {}, "Teilen")),
      h("div", { class: "full share-row" },
        h("button", { class: "btn", type: "button", onclick: () => share.exportAll?.() }, "📤 Alles exportieren…"),
        h("button", { class: "btn", type: "button", onclick: () => share.exportSettings?.() }, "📤 Nur Einstellungen…"),
        h("button", { class: "btn", type: "button", onclick: () => share.importShare?.() }, "📥 Importieren…")),
      h("div", { class: "full hint" }, "Export ohne Benutzer, Passwörter und persönliche Pfade. Einzelne Ordner und Sessions: Rechtsklick im Baum."),
      h("div", { class: "field full section" }, h("span", {}, "Aus anderen Programmen übernehmen")),
      h("div", { class: "full share-row" },
        h("button", { class: "btn", type: "button", onclick: () => { close(null); onImport(); } }, "PuTTY importieren"),
        share.importSecureCRT ? h("button", { class: "btn", type: "button", onclick: () => { close(null); share.importSecureCRT(); } }, "SecureCRT importieren…") : null),
      h("div", { class: "full hint" }, "Ordner, Hosts, Ports und Benutzer kommen mit, Passwörter nicht. Doppelte werden übersprungen."),
      h("div", { class: "field full section" }, h("span", {}, "Session-Logging")),
      log.elements,
      h("div", { class: "field full section" }, h("span", {}, "Hervorhebungen")),
      hl.elements,
      h("div", { class: "field full section" }, h("span", {}, "Running-Configs")),
      configDirRow(s),
      h("div", { class: "full hint" }, "Hier landen gesicherte Running-Configs: Rechtsklick auf einen Tab → „Running-Config sichern“ oder per TFTP-Upload im Firmware-Server."),
      h("div", { class: "field full section" }, h("span", {}, "KI-Zugriff (MCP)")),
      ai.elements,
      h("div", { class: "field full section" }, h("span", {}, "Updates")),
      chk("updateCheck", "Beim Start nach einer neuen Version suchen (fragt nur die Release-Seite, sendet keine Daten)"),
      h("div", { class: "full share-row" },
        h("button", { class: "btn", type: "button", onclick: () => share.checkUpdate?.() }, "Jetzt nach Updates suchen")),
      h("div", { class: "full hint" }, `${info.name || "easyTTY"} ${info.version || ""} · Daten: ${info.dataDir || ""}`),
    )),
    foot: [
      h("button", { class: "btn left", onclick: () => api.OpenDataDir().catch((e) => toast(errMsg(e), "err")) }, "Datenordner"),
      h("button", { class: "btn", onclick: () => close(null) }, "Abbrechen"),
      h("button", { class: "btn primary", onclick: async () => {
        try {
          close(await api.SaveSettings({
            ...s, ...log.collect(), ...ai.collect(), ...hl.collect(), mcpToken: "", fontFamily: font.value() || s.fontFamily, fontSize: parseInt(size.value, 10) || s.fontSize, theme: theme.value,
            scrollback: Number.isFinite(parseInt(scroll.value, 10)) ? parseInt(scroll.value, 10) : s.scrollback, pasteLineDelay: parseInt(pasteDelay.value, 10) || 0,
            nameCase: nameCase.value, nameSpaces: nameSpaces.value,
          }));
        } catch (e) { toast(errMsg(e), "err"); }
      } }, "Speichern"),
    ],
  })).then((v) => v || null);
}

/**
 * Reads "8443 10.0.0.1:443" lines (also "8443:host:443", IPv6 in brackets)
 * into forwards; throws with the line that cannot be read.
 */
function parseForwards(text) {
  const out = [];
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    const m = line.match(/^(\d{1,5})[\s:]+(\[[^\]]+\]|[^\s:\[\]]+):(\d{1,5})$/);
    if (!m) throw new Error(`Weiterleitung „${line}“ nicht lesbar. Beispiel: 8443 10.0.0.1:443`);
    out.push({ listen: +m[1], host: m[2].replace(/^\[|\]$/g, ""), port: +m[3] });
  }
  return out;
}

/** Folder for saved running-configs: shown, chosen (saved at once) and opened. */
function configDirRow(s) {
  const path = h("span", { class: "fw-updir" }, s.configDir || "…");
  api.ConfigDir().then((d) => { path.textContent = d; path.title = d; }).catch((e) => { path.textContent = errMsg(e); });
  return h("div", { class: "full row" }, path,
    h("button", { class: "btn", type: "button", title: "Ordner wählen", onclick: async () => {
      try {
        const d = await api.PickConfigDir();
        path.textContent = d;
        path.title = d;
        s.configDir = d; // "Speichern" below must not put the old folder back
      } catch (e) { toast(errMsg(e), "err"); }
    } }, "…"),
    h("button", { class: "btn", type: "button", title: "Im Explorer öffnen", onclick: () => api.OpenConfigDir().catch((e) => toast(errMsg(e), "err")) }, "Öffnen"));
}

/**
 * Puts a row of jump buttons above a long form: one per ".section" heading,
 * plus `first` for the part before the first heading. The row stays visible
 * while the form scrolls.
 */
function withSectionNav(first, grid) {
  const jump = (label, target) => h("button", {
    type: "button", class: "chip",
    onclick: () => {
      const scroller = wrap.parentElement; // the dialog body
      // Just below the sticky row, wherever the form is scrolled to right now.
      scroller.scrollTop = target ? scroller.scrollTop + target.getBoundingClientRect().top - nav.getBoundingClientRect().bottom - 4 : 0;
    },
  }, label);
  const nav = h("div", { class: "section-nav" }, jump(first, null),
    [...grid.querySelectorAll(":scope > .section")].map((s) => jump(s.textContent.replace(/\s*\(.*\)$/, ""), s)));
  const wrap = h("div", {}, nav, grid);
  return wrap;
}
