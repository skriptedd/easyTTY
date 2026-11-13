// AI access (MCP): approval dialog for everything an AI client wants to send
// to a device, and the settings block with ready-made client configs.
import { api, clipboard, errMsg, events } from "./api.js";
import { h, modal, toast } from "./ui.js";

const open = new Map(); // approval id -> close function

/** Call once at startup. */
export function initMcp() {
  events.on("mcp:approve", (ev) => askApproval(ev));
  events.on("mcp:approve:cancel", (id) => open.get(id)?.("timeout"));
  // Answered here or in another window: close every copy of the dialog.
  events.on("mcp:approve:done", (id) => open.get(id)?.("timeout"));
}

function askApproval(ev) {
  if (open.has(ev.id)) return; // one dialog per request, even if the event arrives twice
  open.set(ev.id, null);
  let left = ev.timeout || 180;
  const countdown = h("span", { class: "muted" });
  const tick = () => { countdown.textContent = `· automatisch abgelehnt in ${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}`; };
  tick();
  const timer = setInterval(() => { left = Math.max(0, left - 1); tick(); }, 1000);
  // Focus starts on "Ablehnen" and the buttons wait a moment, so an Enter
  // still meant for the terminal never approves anything.
  const deny = h("button", { class: "btn left", autofocus: true }, "Ablehnen");
  const grant = ev.grantable ? h("button", { class: "btn", disabled: true }, "Genau das 15 Min. ohne Rückfrage") : null;
  const once = h("button", { class: "btn primary", disabled: true }, "Einmal erlauben");
  setTimeout(() => { once.disabled = false; if (grant) grant.disabled = false; }, 800);
  modal((close) => {
    open.set(ev.id, close);
    deny.onclick = () => close("deny");
    once.onclick = () => close("once");
    if (grant) grant.onclick = () => close("grant");
    return {
      head: `KI-Zugriff: ${ev.client} möchte …`,
      danger: true,
      sticky: true,
      body: h("div", { class: "grid" },
        h("div", { class: "full mcp-ask" }, h("b", {}, ev.action), " auf ", h("b", {}, ev.target)),
        ev.detail ? h("pre", { class: "full mcp-detail" }, ev.detail) : null,
        h("p", { class: "full hint" }, "Prüf genau, was gesendet wird – die KI sieht danach die Ausgabe. ",
          ev.grantable ? "„15 Min.“ erlaubt genau diese Befehle auf genau diesen Geräten ohne weitere Rückfrage. " : "", countdown)),
      foot: [deny, grant, once].filter(Boolean),
    };
  }).then((answer) => {
    clearInterval(timer);
    open.delete(ev.id);
    if (answer !== "timeout") api.AnswerApproval(ev.id, answer || "deny").catch((e) => toast(errMsg(e), "err"));
  });
}

/** Settings block: { elements, collect() } like the logging block. */
export function mcpFields(s) {
  const enabled = h("input", { type: "checkbox", checked: !!s.mcpEnabled });
  const port = h("input", { type: "number", min: 1024, max: 65535, value: s.mcpPort || 47321 });
  const showAuto = h("input", { type: "checkbox", checked: !!s.mcpShowAuto });
  const readTabs = h("input", { type: "checkbox", checked: !!s.mcpReadTabs });
  const speed = h("select", {},
    h("option", { value: "" }, "zum Mitlesen (Standard)"),
    h("option", { value: "slow" }, "langsam"),
    h("option", { value: "fast" }, "schnell (ohne Tipp-Effekt)"));
  speed.value = s.typeSpeed || "";
  const info = h("div", { class: "full mcp-info" });

  const render = (i) => {
    if (!i.enabled) {
      info.replaceChildren(h("div", { class: "hint" }, "Aus. Haken setzen und speichern – danach stehen hier Adresse, Token und fertige Einträge für MCP-Clients."));
      return;
    }
    const masked = h("code", {}, "•".repeat(12));
    const code = `claude mcp add --transport http easytty ${i.url} --header "Authorization: Bearer ${i.token}"`;
    const desktop = JSON.stringify({ mcpServers: { easytty: { command: i.exe, args: ["--mcp"], env: { EASYTTY_MCP_URL: i.url, EASYTTY_MCP_TOKEN: i.token } } } }, null, 2);
    const copy = (text, what) => { clipboard.set(text); toast(`${what} kopiert`); };
    info.replaceChildren(
      h("div", { class: i.running ? "mcp-on" : "err" }, i.running ? `● läuft auf ${i.url} (nur dieser PC)` : `● nicht gestartet${i.error ? ": " + i.error : ""}`),
      h("div", { class: "row mcp-token" }, h("span", {}, "Token:"), masked,
        h("button", { class: "btn small", type: "button", onclick: () => { masked.textContent = masked.textContent.startsWith("•") ? i.token : "•".repeat(12); } }, "zeigen"),
        h("button", { class: "btn small", type: "button", onclick: () => copy(i.token, "Token") }, "kopieren"),
        h("button", { class: "btn small", type: "button", onclick: async () => {
          try { render(await api.NewMcpToken()); toast("Neuer Token – alte Einträge in KI-Clients gehen nicht mehr.", "ok", 6000); } catch (e) { toast(errMsg(e), "err"); }
        } }, "neu erzeugen")),
      h("div", { class: "row" },
        h("button", { class: "btn", type: "button", onclick: () => copy(code, "Befehl") }, "Befehl kopieren"),
        h("button", { class: "btn", type: "button", onclick: () => copy(desktop, "JSON-Eintrag") }, "JSON-Eintrag kopieren")),
      h("div", { class: "hint" }, "Befehl: im Terminal des KI-Clients ausführen. JSON-Eintrag: in dessen Konfiguration unter „mcpServers“ einfügen und den Client neu starten (startet easyTTY.exe --mcp als Brücke, easyTTY muss laufen)."));
  };
  api.GetMcpInfo().then(render).catch((e) => info.replaceChildren(h("div", { class: "err" }, errMsg(e))));

  return {
    elements: [
      h("label", { class: "field full" }, h("span", {}, "Tipptempo, wenn KI oder „Jetzt ausführen“ Befehle sichtbar in einen Tab tippen"), speed),
      h("label", { class: "check full" }, enabled, "KI-Assistenten dürfen easyTTY nutzen: Sessions und Tabs sehen, Befehle ausführen, Aufgaben planen"),
      h("label", { class: "field" }, h("span", {}, "Port (nur 127.0.0.1)"), port),
      h("label", { class: "check" }, showAuto, "Reine show-/display-Befehle ohne Rückfrage"),
      h("label", { class: "check full" }, readTabs, "KI darf mitlesen, was in offenen Tabs steht (Achtung: dort können auch Passwörter oder Configs stehen)"),
      h("div", { class: "full hint" }, "Ohne Rückfrage: Session-Liste (ohne Passwörter), Aufgaben und alte Ergebnisse – also auch früher gesicherte Configs. Alles, was an ein Gerät geht oder Aufgaben ändert, musst du hier in easyTTY freigeben. Zugriff nur mit Token, nur von diesem PC."),
      info,
    ],
    collect: () => ({ mcpEnabled: enabled.checked, mcpPort: parseInt(port.value, 10) || 47321, mcpShowAuto: showAuto.checked, mcpReadTabs: readTabs.checked, typeSpeed: speed.value }),
  };
}
